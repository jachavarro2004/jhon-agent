# Auth Specification

## Requirement: Login

The system MUST authenticate a user with a password or a passkey.

### Scenario: Valid credentials

- GIVEN a registered user
- WHEN they submit the right password
- THEN a session is created
