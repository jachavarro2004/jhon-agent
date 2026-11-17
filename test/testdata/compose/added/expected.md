# Auth Specification

## Purpose

Session handling for the web client.

## Requirements

### Requirement: Login

The system MUST authenticate a user with a password.

#### Scenario: Valid credentials

- GIVEN a registered user
- WHEN they submit the right password
- THEN a session is created

### Requirement: Logout

The system MUST end the session on request.

#### Scenario: Explicit logout

- GIVEN an active session
- WHEN the user logs out
- THEN the session is destroyed

### Requirement: Refresh

The system MUST refresh a session before it expires.

#### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended

## Notes

Anything here is outside every requirement block.
