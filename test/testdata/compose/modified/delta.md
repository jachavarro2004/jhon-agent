# Delta for Auth

## MODIFIED Requirements

### Requirement: Login

The system MUST authenticate a user with a password or a passkey.
(Previously: password only)

#### Scenario: Valid credentials

- GIVEN a registered user
- WHEN they submit the right password
- THEN a session is created

#### Scenario: Passkey

- GIVEN a registered passkey
- WHEN the user presents it
- THEN a session is created
