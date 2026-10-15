-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- Keep existing rows valid before tightening the reward rule columns.
UPDATE activities SET max_daily_completions = 1
WHERE max_daily_completions IS NULL OR max_daily_completions < 1;
UPDATE activities SET streak_eligible = true WHERE streak_eligible IS NULL;
UPDATE activities SET is_active = true WHERE is_active IS NULL;
ALTER TABLE activities
  ALTER COLUMN max_daily_completions SET DEFAULT 1,
  ALTER COLUMN max_daily_completions SET NOT NULL,
  ADD CONSTRAINT activities_max_daily_completions_positive
    CHECK (max_daily_completions > 0),
  ALTER COLUMN streak_eligible SET DEFAULT true,
  ALTER COLUMN streak_eligible SET NOT NULL,
  ALTER COLUMN is_active SET DEFAULT true,
  ALTER COLUMN is_active SET NOT NULL;

UPDATE streak_milestones SET is_active = true WHERE is_active IS NULL;
ALTER TABLE streak_milestones
  ALTER COLUMN is_active SET DEFAULT true,
  ALTER COLUMN is_active SET NOT NULL;

ALTER TABLE activity_completions
  ADD COLUMN idempotency_key varchar(200),
  ADD COLUMN current_streak_at_completion smallint NOT NULL DEFAULT 0,
  ADD COLUMN milestone_achieved boolean NOT NULL DEFAULT false,
  ADD COLUMN milestone_bonus_awarded smallint NOT NULL DEFAULT 0;
CREATE UNIQUE INDEX idx_activity_completions_account_idempotency
  ON activity_completions(account_id, idempotency_key)
  WHERE idempotency_key IS NOT NULL;

-- Lock per account/activity before checking the daily limit. This serializes
-- concurrent completions while allowing unrelated activities to proceed.
-- +goose StatementBegin
CREATE FUNCTION record_activity_completion(
    p_account_id uuid,
    p_activity_id uuid,
    p_metadata jsonb,
    p_idempotency_key text,
    p_awarded_by text
)
RETURNS TABLE(
    completion_id bigint,
    points_earned smallint,
    current_streak smallint,
    milestone_achieved boolean,
    milestone_bonus smallint,
    already_processed boolean
) AS $$
DECLARE
    v_activity RECORD;
    v_completions_today int;
    v_points smallint;
    v_completion_id bigint;
    v_user_streak RECORD;
    v_new_streak smallint;
    v_milestone_id uuid;
    v_milestone_bonus smallint := 0;
    v_milestone_achieved boolean := false;
    v_today date := CURRENT_DATE;
    v_yesterday date := CURRENT_DATE - INTERVAL '1 day';
    v_existing_activity_id uuid;
    v_existing_streak smallint;
    v_existing_milestone_achieved boolean;
    v_existing_milestone_bonus smallint;
BEGIN
    IF p_idempotency_key IS NOT NULL THEN
        IF length(btrim(p_idempotency_key)) = 0 OR length(p_idempotency_key) > 200 THEN
            RAISE EXCEPTION 'Invalid idempotency key';
        END IF;
        PERFORM pg_advisory_xact_lock(
          hashtextextended(
            p_account_id::text || ':idempotency:' || p_idempotency_key,
            0
          )
        );
    END IF;

    PERFORM pg_advisory_xact_lock(
      hashtextextended(p_account_id::text || ':' || p_activity_id::text, 0)
    );

    IF p_idempotency_key IS NOT NULL THEN
        SELECT
          ac.id,
          ac.activity_id,
          ac.points_earned,
          ac.current_streak_at_completion,
          ac.milestone_achieved,
          ac.milestone_bonus_awarded
        INTO
          v_completion_id,
          v_existing_activity_id,
          v_points,
          v_existing_streak,
          v_existing_milestone_achieved,
          v_existing_milestone_bonus
        FROM activity_completions ac
        WHERE ac.account_id = p_account_id
          AND ac.idempotency_key = p_idempotency_key;

        IF FOUND THEN
            IF v_existing_activity_id <> p_activity_id THEN
                RAISE EXCEPTION 'Idempotency key was already used for another activity';
            END IF;

            RETURN QUERY SELECT
              v_completion_id,
              v_points,
              v_existing_streak,
              v_existing_milestone_achieved,
              v_existing_milestone_bonus,
              true;
            RETURN;
        END IF;
    END IF;

    SELECT * INTO v_activity
    FROM activities
    WHERE id = p_activity_id AND is_active = true;

    IF NOT FOUND THEN
        RAISE EXCEPTION 'Activity not found or inactive';
    END IF;

    SELECT COUNT(*) INTO v_completions_today
    FROM activity_completions
    WHERE account_id = p_account_id
      AND activity_id = p_activity_id
      AND completion_date = v_today;

    IF v_completions_today >= v_activity.max_daily_completions THEN
        RAISE EXCEPTION 'Daily completion limit reached for this activity';
    END IF;

    v_points := v_activity.points_awarded;

    INSERT INTO activity_completions (
      account_id, activity_id, points_earned, metadata, idempotency_key
    ) VALUES (
      p_account_id, p_activity_id, v_points, p_metadata, p_idempotency_key
    ) RETURNING id INTO v_completion_id;

    INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
    VALUES (
      p_account_id,
      'Activity: ' || v_activity.name,
      v_points,
      COALESCE(NULLIF(p_awarded_by, ''), 'system')
    );

    IF v_activity.streak_eligible THEN
        INSERT INTO user_streaks (account_id, activity_id)
        VALUES (p_account_id, p_activity_id)
        ON CONFLICT (account_id, activity_id) DO NOTHING;

        SELECT * INTO v_user_streak
        FROM user_streaks
        WHERE account_id = p_account_id AND activity_id = p_activity_id
        FOR UPDATE;

        IF v_user_streak.last_completion_date IS NULL THEN
            v_new_streak := 1;
            UPDATE user_streaks
            SET current_streak = 1,
                longest_streak = 1,
                last_completion_date = v_today,
                streak_started_at = v_today,
                total_completions = 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
        ELSIF v_user_streak.last_completion_date = v_today THEN
            v_new_streak := v_user_streak.current_streak;
            UPDATE user_streaks
            SET total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
        ELSIF v_user_streak.last_completion_date = v_yesterday THEN
            v_new_streak := v_user_streak.current_streak + 1;
            UPDATE user_streaks
            SET current_streak = v_new_streak,
                longest_streak = GREATEST(longest_streak, v_new_streak),
                last_completion_date = v_today,
                total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
        ELSE
            v_new_streak := 1;
            UPDATE user_streaks
            SET current_streak = 1,
                last_completion_date = v_today,
                streak_started_at = v_today,
                total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
        END IF;

        SELECT sm.id, sm.bonus_points
        INTO v_milestone_id, v_milestone_bonus
        FROM streak_milestones sm
        LEFT JOIN user_streak_achievements usa
          ON usa.streak_milestone_id = sm.id AND usa.account_id = p_account_id
        WHERE sm.activity_id = p_activity_id
          AND sm.is_active = true
          AND sm.days_required = v_new_streak
          AND usa.id IS NULL
        LIMIT 1;

        IF FOUND THEN
            INSERT INTO user_streak_achievements (
              account_id, streak_milestone_id, user_streak_id, bonus_points_awarded
            ) VALUES (
              p_account_id, v_milestone_id, v_user_streak.id, v_milestone_bonus
            );

            INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
            VALUES (
              p_account_id,
              'Streak Milestone: ' || v_new_streak || ' days',
              v_milestone_bonus,
              COALESCE(NULLIF(p_awarded_by, ''), 'system')
            );

            v_milestone_achieved := true;
        END IF;
    ELSE
        v_new_streak := 0;
        v_milestone_bonus := 0;
    END IF;

    UPDATE activity_completions
    SET current_streak_at_completion = v_new_streak,
        milestone_achieved = v_milestone_achieved,
        milestone_bonus_awarded = v_milestone_bonus
    WHERE id = v_completion_id;

    RETURN QUERY SELECT
      v_completion_id,
      v_points,
      v_new_streak,
      v_milestone_achieved,
      v_milestone_bonus,
      false;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION get_user_streaks(p_account_id uuid)
RETURNS TABLE(
    activity_name varchar(255),
    current_streak smallint,
    longest_streak smallint,
    total_completions int,
    last_completion_date date,
    days_until_next_milestone smallint
) AS $$
BEGIN
    RETURN QUERY
    SELECT
      a.name,
      CASE
        WHEN us.last_completion_date < CURRENT_DATE THEN 0::smallint
        ELSE us.current_streak
      END,
      us.longest_streak,
      us.total_completions,
      us.last_completion_date,
      (
        SELECT MIN(sm.days_required -
          CASE
            WHEN us.last_completion_date < CURRENT_DATE THEN 0
            ELSE us.current_streak
          END)
        FROM streak_milestones sm
        LEFT JOIN user_streak_achievements usa
          ON usa.streak_milestone_id = sm.id AND usa.account_id = p_account_id
        WHERE sm.activity_id = a.id
          AND sm.is_active = true
          AND sm.days_required >
            CASE
              WHEN us.last_completion_date < CURRENT_DATE THEN 0
              ELSE us.current_streak
            END
          AND usa.id IS NULL
      )::smallint
    FROM user_streaks us
    JOIN activities a ON a.id = us.activity_id
    WHERE us.account_id = p_account_id
    ORDER BY us.current_streak DESC;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

CREATE OR REPLACE VIEW account_vibepoint_rank AS
SELECT
  id,
  email,
  name,
  username,
  vibe_points,
  avatar_url,
  created_at,
  updated_at,
  RANK() OVER (ORDER BY vibe_points DESC) AS vibe_rank
FROM accounts
WHERE type = 'human' AND deleted_at IS NULL;

INSERT INTO permissions (name, description)
VALUES
  ('create:activity:any', 'Create reward activity definitions.'),
  ('read:activity:any', 'Read any account activity completion history.'),
  ('update:activity:any', 'Update reward activity definitions.'),
  ('delete:activity:any', 'Delete reward activity definitions.'),
  ('create:streak_milestone:any', 'Create streak reward milestones.'),
  ('delete:streak_milestone:any', 'Delete streak reward milestones.'),
  ('award:activity:any', 'Award activity rewards to any account from a trusted service.')
ON CONFLICT (name) DO NOTHING;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

DELETE FROM permissions
WHERE name IN (
  'create:activity:any',
  'read:activity:any',
  'update:activity:any',
  'delete:activity:any',
  'create:streak_milestone:any',
  'delete:streak_milestone:any',
  'award:activity:any'
);

DROP VIEW IF EXISTS account_vibepoint_rank;
CREATE VIEW account_vibepoint_rank AS
SELECT
  id,
  email,
  name,
  username,
  vibe_points,
  avatar_url,
  created_at,
  updated_at,
  RANK() OVER (ORDER BY vibe_points DESC) AS vibe_rank
FROM accounts
WHERE type = 'human';

-- Restore the pre-migration helper function when rolling this migration back.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION get_user_streaks(p_account_id uuid)
RETURNS TABLE(
    activity_name varchar(255),
    current_streak smallint,
    longest_streak smallint,
    total_completions int,
    last_completion_date date,
    days_until_next_milestone smallint
) AS $$
BEGIN
    RETURN QUERY
    SELECT
      a.name,
      us.current_streak,
      us.longest_streak,
      us.total_completions,
      us.last_completion_date,
      (
        SELECT MIN(sm.days_required - us.current_streak)
        FROM streak_milestones sm
        LEFT JOIN user_streak_achievements usa
          ON usa.streak_milestone_id = sm.id AND usa.account_id = p_account_id
        WHERE sm.activity_id = a.id
          AND sm.is_active = true
          AND sm.days_required > us.current_streak
          AND usa.id IS NULL
      )::smallint
    FROM user_streaks us
    JOIN activities a ON a.id = us.activity_id
    WHERE us.account_id = p_account_id
    ORDER BY us.current_streak DESC;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

DROP FUNCTION IF EXISTS record_activity_completion(uuid, uuid, jsonb, text, text);
DROP INDEX IF EXISTS idx_activity_completions_account_idempotency;
ALTER TABLE activity_completions DROP COLUMN IF EXISTS idempotency_key;
ALTER TABLE activity_completions
  DROP COLUMN IF EXISTS current_streak_at_completion,
  DROP COLUMN IF EXISTS milestone_achieved,
  DROP COLUMN IF EXISTS milestone_bonus_awarded;
ALTER TABLE streak_milestones ALTER COLUMN is_active DROP NOT NULL;
ALTER TABLE activities DROP CONSTRAINT IF EXISTS activities_max_daily_completions_positive;
ALTER TABLE activities
  ALTER COLUMN max_daily_completions DROP NOT NULL,
  ALTER COLUMN streak_eligible DROP NOT NULL,
  ALTER COLUMN is_active DROP NOT NULL;
