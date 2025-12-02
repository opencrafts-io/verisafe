-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- Activity definitions (what users can do to earn points)
CREATE TABLE activities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name varchar(255) NOT NULL,
    description text,
    category varchar(100), -- e.g., 'social', 'content', 'engagement'
    points_awarded smallint NOT NULL CHECK (points_awarded > 0 AND points_awarded <= 10),
    max_daily_completions smallint DEFAULT 1, -- How many times per day this can be completed
    streak_eligible boolean DEFAULT true, -- Can this activity contribute to streaks?
    is_active boolean DEFAULT true,
    created_at timestamp DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_activities_active ON activities(is_active) WHERE is_active = true;
CREATE INDEX idx_activities_streak_eligible ON activities(streak_eligible) WHERE streak_eligible = true;


-- User activity completions (tracking individual completions)
CREATE TABLE activity_completions (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    completed_at timestamp DEFAULT CURRENT_TIMESTAMP,
    completion_date date GENERATED ALWAYS AS (completed_at::date) STORED, -- For efficient date-based queries
    points_earned smallint NOT NULL,
    metadata jsonb -- Store additional context (e.g., which post, which action, etc.)
);

CREATE INDEX idx_activity_completions_account_activity ON activity_completions(account_id, activity_id);
CREATE INDEX idx_activity_completions_account_date ON activity_completions(account_id, completion_date DESC);
CREATE INDEX idx_activity_completions_date ON activity_completions(completion_date DESC);



-- User streaks (one row per user per activity they're tracking)
CREATE TABLE user_streaks (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    current_streak smallint NOT NULL DEFAULT 0, -- Current consecutive days
    longest_streak smallint NOT NULL DEFAULT 0, -- Best streak ever
    last_completion_date date, -- Last day they completed this activity
    streak_started_at date, -- When current streak began
    total_completions int NOT NULL DEFAULT 0, -- Lifetime completions
    created_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    updated_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(account_id, activity_id)
);

CREATE INDEX idx_user_streaks_account ON user_streaks(account_id);
CREATE INDEX idx_user_streaks_current_streak ON user_streaks(current_streak DESC);
CREATE INDEX idx_user_streaks_longest_streak ON user_streaks(longest_streak DESC);


-- Streak milestones (bonus points for reaching streak milestones)
CREATE TABLE streak_milestones (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    activity_id uuid REFERENCES activities(id) ON DELETE CASCADE,
    days_required smallint NOT NULL, -- e.g., 7, 30, 100
    bonus_points smallint NOT NULL CHECK (bonus_points > 0 AND bonus_points <= 10),
    title varchar(255) NOT NULL, -- e.g., "Week Warrior", "Month Master"
    description text,
    is_active boolean DEFAULT true,
    UNIQUE(activity_id, days_required)
);

CREATE INDEX idx_streak_milestones_active ON streak_milestones(is_active) WHERE is_active = true;

-- Track when users achieve streak milestones
CREATE TABLE user_streak_achievements (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
    streak_milestone_id uuid NOT NULL REFERENCES streak_milestones(id) ON DELETE CASCADE,
    user_streak_id bigint NOT NULL REFERENCES user_streaks(id) ON DELETE CASCADE,
    achieved_at timestamp without time zone DEFAULT CURRENT_TIMESTAMP,
    bonus_points_awarded smallint NOT NULL,
    UNIQUE(account_id, streak_milestone_id) -- Only award once per milestone per user
);

CREATE INDEX idx_user_streak_achievements_account ON user_streak_achievements(account_id);
CREATE INDEX idx_user_streak_achievements_achieved_at ON user_streak_achievements(achieved_at DESC);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';

DROP INDEX IF EXISTS idx_user_streak_achievements_account;
DROP INDEX IF EXISTS idx_user_streak_achievements_achieved_at;
DROP TABLE IF EXISTS user_streak_achievements;

-- Drop streak_milestones
DROP INDEX IF EXISTS idx_streak_milestones_active;
DROP TABLE IF EXISTS streak_milestones;

-- Drop user streaks
DROP INDEX IF EXISTS idx_user_streaks_longest_streak;
DROP INDEX IF EXISTS idx_user_streaks_current_streak;
DROP INDEX IF EXISTS idx_user_streaks_account;
DROP TABLE IF EXISTS user_streaks;

-- Drop the activity_completions table
DROP INDEX IF EXISTS idx_activity_completions_date;
DROP INDEX IF EXISTS idx_activity_completions_account_date;
DROP INDEX IF EXISTS idx_activity_completions_account_activity;
DROP TABLE IF EXISTS activity_completions;

-- Drop the base activities table
DROP INDEX IF EXISTS idx_activities_active;
DROP INDEX IF EXISTS idx_activities_streak_eligible;
DROP TABLE IF EXISTS activities;

-- +goose StatementEnd
