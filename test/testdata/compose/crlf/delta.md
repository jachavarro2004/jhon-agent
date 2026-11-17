# Delta for Auth

## ADDED Requirements

### Requirement: Refresh

The system MUST refresh a session before it expires.

#### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended

## MODIFIED Requirements

### Requirement: Sign Out

The system MUST end the session on request and revoke its tokens.
(Previously: tokens were not revoked)

#### Scenario: Explicit sign out

- GIVEN an active session
- WHEN the user signs out
- THEN the session and its tokens are revoked

## REMOVED Requirements

### Requirement: Login

(Reason: authentication moved to the identity domain)

## RENAMED Requirements

### Requirement: Logout → Sign Out

(Reason: product vocabulary)
(Migration: rename references in tests)
