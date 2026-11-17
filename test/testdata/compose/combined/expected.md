# Auth Specification

## Purpose

Session handling for the web client.

## Requirements

### Requirement: Sign Out

The system MUST end the session on request and revoke its tokens.

#### Scenario: Explicit sign out

- GIVEN an active session
- WHEN the user signs out
- THEN the session and its tokens are revoked

### Requirement: Refresh

The system MUST refresh a session before it expires.

#### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended

## Notes

Anything here is outside every requirement block.
