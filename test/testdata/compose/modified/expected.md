# Auth Specification

## Purpose

Session handling for the web client.

## Requirements

### Requirement: Login

The system MUST authenticate a user with a password or a passkey.

#### Scenario: Valid credentials

- GIVEN a registered user
- WHEN they submit the right password
- THEN a session is created

#### Scenario: Passkey

- GIVEN a registered passkey
- WHEN the user presents it
- THEN a session is created

### Requirement: Logout

The system MUST end the session on request.

#### Scenario: Explicit logout

- GIVEN an active session
- WHEN the user logs out
- THEN the session is destroyed

## Notes

Anything here is outside every requirement block.
