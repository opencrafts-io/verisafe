# 11. Add password authentication for existing human accounts

Date: 2026-09-26

## Status

accepted

## Context

Verisafe currently authenticates human accounts through third-party OAuth providers.
That prevents app reviewers and users who do not want to use a provider login from
signing in. Existing accounts need to be able to add a password without changing
their account ID, OAuth connections, roles, or data ownership.

## Decision

Add an optional password credential for existing `human` accounts in a separate
`account_password_credentials` table, keyed by account ID. Passwords are stored as
Argon2id hashes. Add an authenticated endpoint that lets the signed-in account set
or replace its password, and a public email/password login endpoint that issues the
existing Verisafe access and refresh token pair and records a device. Login does not
create accounts.

Apply Redis-backed per-IP and per-IP/email attempt limits to password login. Return
the same authentication error for an unknown email and an incorrect password.
Password recovery, account creation by password, and removal of OAuth login are
outside this decision.

## Consequences

Existing accounts can use both OAuth and password login while retaining the same
identity and authorization state. The database change is additive, and password
credentials can be deleted independently of account data.

The service gains password hashing, credential management, and a login rate-limit
dependency on Redis. Clients must add password setup and login screens. Users who
lose both their active sessions and provider access cannot recover their password
until a separate recovery flow is designed.
