# Auth Specification

## Requirements

### Requirement: Login

The system MUST authenticate a user with a password.

```markdown
## Requirement: Not a heading
### Scenario: Not a heading either
```

#### Scenario: Valid credentials

- THEN a session is created

### Requirement: Refresh

The system MUST refresh a session before it expires.

#### Scenario: Near expiry

- GIVEN a session within five minutes of expiry
- WHEN the client makes a request
- THEN the session is extended
