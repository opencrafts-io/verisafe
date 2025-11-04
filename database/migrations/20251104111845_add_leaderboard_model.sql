-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
CREATE TABLE vibepoint_transactions (
  id BIGSERIAL PRIMARY KEY,
  account_id UUID NOT NULL,
  awarding_reason TEXT,
  points_awarded SMALLINT NOT NULL CHECK ( points_awarded > -11 AND points_awarded < 11 ),
  awarded_at  TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  awarded_by VARCHAR(255),
  FOREIGN KEY(account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- +goose StatementBegin
-- Create the trigger function that updates account vibe_points
CREATE OR REPLACE FUNCTION update_account_vibe_points()
RETURNS TRIGGER AS $$
BEGIN
    -- Update the account's vibe_points by adding the points_awarded
    UPDATE accounts
    SET vibe_points = vibe_points + NEW.points_awarded,
        updated_at = CURRENT_TIMESTAMP
    WHERE id = NEW.account_id;
    
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create the trigger that fires after insert on vibepoint_transactions
CREATE TRIGGER trigger_update_vibe_points
    AFTER INSERT ON vibepoint_transactions
    FOR EACH ROW
    EXECUTE FUNCTION update_account_vibe_points();
-- +goose StatementEnd


-- +goose StatementBegin
-- Award 1 vibe point per day since account creation for all existing accounts
INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
SELECT 
    id as account_id,
    'Daily vibe point for ' || EXTRACT(DAY FROM (CURRENT_TIMESTAMP - created_at))::INTEGER || ' days since account creation' as awarding_reason,
    LEAST(EXTRACT(DAY FROM (CURRENT_TIMESTAMP - created_at))::INTEGER, 10) as points_awarded,
    'io.opencrafts.verisafe' as awarded_by
FROM accounts
WHERE created_at IS NOT NULL
  AND EXTRACT(DAY FROM (CURRENT_TIMESTAMP - created_at))::INTEGER > 0;

-- Note: Using LEAST to cap at 10 points per transaction due to CHECK constraint
-- If a user has been around longer than 10 days, we'll need multiple transactions

-- For users with more than 10 days, insert additional transactions
DO $$
DECLARE
    account_record RECORD;
    days_since_creation INTEGER;
    remaining_days INTEGER;
    points_to_award INTEGER;
BEGIN
    FOR account_record IN 
        SELECT id, created_at 
        FROM accounts 
        WHERE created_at IS NOT NULL
    LOOP
        days_since_creation := EXTRACT(DAY FROM (CURRENT_TIMESTAMP - account_record.created_at))::INTEGER;
        
        -- If more than 10 days, we need multiple transactions
        IF days_since_creation > 10 THEN
            remaining_days := days_since_creation - 10; -- Already inserted 10 above
            
            -- Insert transactions in batches of 10 points
            WHILE remaining_days > 0 LOOP
                points_to_award := LEAST(remaining_days, 10);
                
                INSERT INTO vibepoint_transactions (account_id, awarding_reason, points_awarded, awarded_by)
                VALUES (
                    account_record.id,
                    'Daily vibe point batch (' || points_to_award || ' days)',
                    points_to_award,
                    'io.opencrafts.verisafe'
                ); 
                remaining_days := remaining_days - points_to_award;
            END LOOP;
        END IF;
    END LOOP;
END $$;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- Remove all system-awarded daily vibe points
DELETE FROM vibepoint_transactions 
WHERE awarded_by = 'io.opencrafts.verisafe' 
  AND awarding_reason LIKE 'Daily vibe point%';

-- Drop the trigger
DROP TRIGGER IF EXISTS trigger_update_vibe_points ON vibepoint_transactions;
-- Drop the function
DROP FUNCTION IF EXISTS update_account_vibe_points();
DROP TABLE IF EXISTS vibepoint_transactions;
-- +goose StatementEnd
