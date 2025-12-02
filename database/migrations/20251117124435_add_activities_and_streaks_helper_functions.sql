-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- +goose StatementBegin
-- Function to record an activity completion and update streaks
CREATE OR REPLACE FUNCTION record_activity_completion(
    p_account_id uuid,
    p_activity_id uuid,
    p_metadata jsonb DEFAULT NULL
)
RETURNS TABLE(
    completion_id bigint,
    points_earned smallint,
    current_streak smallint,
    milestone_achieved boolean,
    milestone_bonus smallint
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
BEGIN
    -- Get activity details
    SELECT * INTO v_activity FROM activities WHERE id = p_activity_id AND is_active = true;
    
    IF NOT FOUND THEN
        RAISE EXCEPTION 'Activity not found or inactive';
    END IF;
    
    -- Check daily completion limit
    SELECT COUNT(*) INTO v_completions_today
    FROM activity_completions
    WHERE account_id = p_account_id
      AND activity_id = p_activity_id
      AND completion_date = v_today;
    
    IF v_completions_today >= v_activity.max_daily_completions THEN
        RAISE EXCEPTION 'Daily completion limit reached for this activity';
    END IF;
    
    v_points := v_activity.points_awarded;
    
    -- Record the completion
    INSERT INTO activity_completions (account_id, activity_id, points_earned, metadata)
    VALUES (p_account_id, p_activity_id, v_points, p_metadata)
    RETURNING id INTO v_completion_id;
    
    -- Award vibepoints
    INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
    VALUES (p_account_id, 'Activity: ' || v_activity.name, v_points, 'system');
    
    -- Handle streaks if eligible
    IF v_activity.streak_eligible THEN
        -- Get or create user streak
        INSERT INTO user_streaks (account_id, activity_id)
        VALUES (p_account_id, p_activity_id)
        ON CONFLICT (account_id, activity_id) DO NOTHING;
        
        SELECT * INTO v_user_streak
        FROM user_streaks
        WHERE account_id = p_account_id AND activity_id = p_activity_id;
        
        -- Update streak logic
        IF v_user_streak.last_completion_date IS NULL THEN
            -- First completion ever
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
            -- Already completed today, no streak change
            v_new_streak := v_user_streak.current_streak;
            UPDATE user_streaks
            SET total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
            
        ELSIF v_user_streak.last_completion_date = v_yesterday THEN
            -- Consecutive day, increment streak
            v_new_streak := v_user_streak.current_streak + 1;
            UPDATE user_streaks
            SET current_streak = v_new_streak,
                longest_streak = GREATEST(longest_streak, v_new_streak),
                last_completion_date = v_today,
                total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
            
        ELSE
            -- Streak broken, restart
            v_new_streak := 1;
            UPDATE user_streaks
            SET current_streak = 1,
                last_completion_date = v_today,
                streak_started_at = v_today,
                total_completions = total_completions + 1,
                updated_at = CURRENT_TIMESTAMP
            WHERE id = v_user_streak.id;
        END IF;
        
        -- Check for milestone achievements
        SELECT sm.id, sm.bonus_points INTO v_milestone_id, v_milestone_bonus
        FROM streak_milestones sm
        LEFT JOIN user_streak_achievements usa ON usa.streak_milestone_id = sm.id 
            AND usa.account_id = p_account_id
        WHERE sm.activity_id = p_activity_id
          AND sm.is_active = true
          AND sm.days_required = v_new_streak
          AND usa.id IS NULL -- Not yet achieved
        LIMIT 1;
        
        IF FOUND THEN
            -- Award milestone bonus
            INSERT INTO user_streak_achievements (account_id, streak_milestone_id, user_streak_id, bonus_points_awarded)
            VALUES (p_account_id, v_milestone_id, v_user_streak.id, v_milestone_bonus);
            
            INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
            VALUES (p_account_id, 'Streak Milestone: ' || v_new_streak || ' days', v_milestone_bonus, 'system');
            
            v_milestone_achieved := true;
        END IF;
    ELSE
        v_new_streak := 0;
    END IF;
    
    RETURN QUERY SELECT v_completion_id, v_points, v_new_streak, v_milestone_achieved, v_milestone_bonus;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd


-- +goose StatementBegin
-- Function to get user's current streaks
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
            LEFT JOIN user_streak_achievements usa ON usa.streak_milestone_id = sm.id 
                AND usa.account_id = p_account_id
            WHERE sm.activity_id = a.id
              AND sm.is_active = true
              AND sm.days_required > us.current_streak
              AND usa.id IS NULL
        )::smallint AS days_until_next_milestone
    FROM user_streaks us
    JOIN activities a ON a.id = us.activity_id
    WHERE us.account_id = p_account_id
    ORDER BY us.current_streak DESC;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
DROP FUNCTION IF EXISTS record_activity_completion;
DROP FUNCTION IF EXISTS get_user_streaks;
-- +goose StatementEnd
