# Delta for Auth

## MODIFIED Requirements

### Requirement: Logout

The system MUST end the session on request or after idling.
(Previously: on request only)

#### Scenario: Explicit logout

- GIVEN an active session
- WHEN the user logs out
- THEN the session is destroyed

## ADDED Requirements

### Requirement: Refresh

The system MUST refresh a session before it expires.

#### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended
