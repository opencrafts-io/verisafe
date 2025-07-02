-- +goose Up
-- +goose StatementBegin
SELECT 'up SQL query';
-- +goose StatementEnd
-- Setup uuid 
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";


CREATE TABLE accounts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  email VARCHAR(255) UNIQUE NOT NULL,
  name VARCHAR(255) NOT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);


-- Create the 'socials' table to store social login details
CREATE TABLE socials (
  user_id VARCHAR(255) PRIMARY KEY,
  id_token TEXT,
  account_id UUID NOT NULL,
  provider VARCHAR(50) NOT NULL,
  email VARCHAR(255),
  name VARCHAR(255),
  first_name VARCHAR(255),
  last_name VARCHAR(255),
  nick_name VARCHAR(255),
  description TEXT,
  avatar_url TEXT,
  location VARCHAR(255),
  access_token TEXT,
  access_token_secret TEXT,
  refresh_token TEXT,
  expires_at TIMESTAMP,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (account_id) REFERENCES accounts(id) ON DELETE CASCADE
);

-- +goose Down
-- +goose StatementBegin
SELECT 'down SQL query';
-- +goose StatementEnd
DROP TABLE IF EXISTS socials;
DROP TABLE IF EXISTS accounts;
