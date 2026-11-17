# Auth Specification

## Requirement: Login

The system MUST authenticate a user with a password.

### Scenario: Valid credentials

- GIVEN a registered user
- WHEN they submit the right password
- THEN a session is created

## Requirement: Refresh

The system MUST refresh a session before it expires.

### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended
