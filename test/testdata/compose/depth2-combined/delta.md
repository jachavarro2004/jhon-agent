# Delta for Billing

## RENAMED Requirements

### Requirement: Charge → Capture

(Reason: processor vocabulary)

## MODIFIED Requirements

### Requirement: Capture

The system MUST capture an authorised card payment.
(Previously: the system charged a card directly)

#### Scenario: Valid card

- GIVEN an authorised card
- THEN the payment is captured

## REMOVED Requirements

### Requirement: Invoice

(Reason: invoicing moved to the accounting domain)
(Migration: none)

## ADDED Requirements

### Requirement: Dispute

The system MUST record a dispute.

#### Scenario: Chargeback

- GIVEN a captured payment
- WHEN the bank opens a chargeback
- THEN a dispute is recorded

```markdown
### Requirement: Example inside a fence
#### Scenario: stays as written
```
