-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd

-- Add new columns to service_tokens table for enhanced security and management
ALTER TABLE service_tokens 
ADD COLUMN IF NOT EXISTS description TEXT,
ADD COLUMN IF NOT EXISTS scopes TEXT[], -- Array of permission scopes
ADD COLUMN IF NOT EXISTS max_uses INTEGER, -- Maximum number of uses (NULL = unlimited)
ADD COLUMN IF NOT EXISTS use_count INTEGER DEFAULT 0, -- Current usage count
ADD COLUMN IF NOT EXISTS rotation_policy JSONB, -- Rotation policy configuration
ADD COLUMN IF NOT EXISTS ip_whitelist TEXT[], -- Allowed IP addresses (NULL = any)
ADD COLUMN IF NOT EXISTS user_agent_pattern TEXT, -- Allowed user agent pattern
ADD COLUMN IF NOT EXISTS created_by UUID REFERENCES accounts(id), -- Who created this token
ADD COLUMN IF NOT EXISTS metadata JSONB; -- Additional metadata

-- Create index for better performance on common queries
CREATE INDEX IF NOT EXISTS idx_service_tokens_account_id_active 
ON service_tokens(account_id) 
WHERE revoked_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_service_tokens_last_used 
ON service_tokens(last_used_at) 
WHERE revoked_at IS NULL;

-- Create a view for active service tokens
CREATE OR REPLACE VIEW active_service_tokens AS
SELECT 
    st.*,
    a.email as account_email,
    a.name as account_name,
    a.type as account_type
FROM service_tokens st
JOIN accounts a ON st.account_id = a.id
WHERE st.revoked_at IS NULL 
  AND (st.expires_at IS NULL OR st.expires_at > NOW())
  AND (st.max_uses IS NULL OR st.use_count < st.max_uses);

-- Create a function to automatically rotate tokens based on policy
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION auto_rotate_service_tokens()
RETURNS void AS $$
DECLARE
    token_record RECORD;
BEGIN
    FOR token_record IN 
        SELECT st.*, a.type as account_type
        FROM service_tokens st
        JOIN accounts a ON st.account_id = a.id
        WHERE st.revoked_at IS NULL 
          AND st.rotation_policy IS NOT NULL
          AND st.rotation_policy->>'auto_rotate' = 'true'
          AND st.rotation_policy->>'rotation_interval_days' IS NOT NULL
          AND st.rotated_at IS NOT NULL
          AND st.rotated_at + (st.rotation_policy->>'rotation_interval_days')::INTEGER * INTERVAL '1 day' < NOW()
    LOOP
        -- Mark token for rotation (actual rotation will be done by application)
        UPDATE service_tokens 
        SET metadata = COALESCE(metadata, '{}'::jsonb) || '{"needs_rotation": true}'::jsonb
        WHERE id = token_record.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Create a function to clean up expired tokens
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION cleanup_expired_service_tokens()
RETURNS void AS $$
BEGIN
    -- Soft delete expired tokens (set revoked_at)
    UPDATE service_tokens 
    SET revoked_at = NOW()
    WHERE revoked_at IS NULL 
      AND expires_at IS NOT NULL 
      AND expires_at < NOW();
END;
$$ LANGUAGE plpgsql;
-- +goose StatementEnd

-- Add new permissions for enhanced service token management
INSERT INTO permissions (name, description) VALUES
  ('read:service_token:own', 'Permission to read own service tokens'),
  ('list:service_token:own', 'Permission to list own service tokens'),
  ('update:service_token:own', 'Permission to update own service tokens'),
  ('rotate:service_token:own', 'Permission to rotate own service tokens'),
  ('revoke:service_token:own', 'Permission to revoke own service tokens'),
  ('read:service_token:any', 'Permission to read any service tokens (admin)'),
  ('list:service_token:any', 'Permission to list any service tokens (admin)'),
  ('update:service_token:any', 'Permission to update any service tokens (admin)'),
  ('rotate:service_token:any', 'Permission to rotate any service tokens (admin)'),
  ('revoke:service_token:any', 'Permission to revoke any service tokens (admin)')
ON CONFLICT (name) DO NOTHING;

-- Assign new permissions to bot role
INSERT INTO role_permissions (role_id, permission_id)
SELECT r.id, p.id
FROM roles r, permissions p
WHERE r.name = 'bot'
  AND p.name IN (
    'read:service_token:own',
    'list:service_token:own',
    'update:service_token:own',
    'rotate:service_token:own',
    'revoke:service_token:own'
  )
ON CONFLICT DO NOTHING;

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd

-- Remove new columns
ALTER TABLE service_tokens 
DROP COLUMN IF EXISTS description,
DROP COLUMN IF EXISTS scopes,
DROP COLUMN IF EXISTS max_uses,
DROP COLUMN IF EXISTS use_count,
DROP COLUMN IF EXISTS rotation_policy,
DROP COLUMN IF EXISTS ip_whitelist,
DROP COLUMN IF EXISTS user_agent_pattern,
DROP COLUMN IF EXISTS created_by,
DROP COLUMN IF EXISTS metadata;

-- Drop indexes
DROP INDEX IF EXISTS idx_service_tokens_account_id_active;
DROP INDEX IF EXISTS idx_service_tokens_last_used;

-- Drop views
DROP VIEW IF EXISTS active_service_tokens;

-- Drop functions
DROP FUNCTION IF EXISTS auto_rotate_service_tokens();
DROP FUNCTION IF EXISTS cleanup_expired_service_tokens();

-- Remove new permissions
DELETE FROM permissions
WHERE name IN (
  'read:service_token:own',
  'list:service_token:own',
  'update:service_token:own',
  'rotate:service_token:own',
  'revoke:service_token:own',
  'read:service_token:any',
  'list:service_token:any',
  'update:service_token:any',
  'rotate:service_token:any',
  'revoke:service_token:any'
);

