# Billing Specification  

## Purpose

Charges and refunds.	

## Requirement: Capture

The system MUST capture an authorised card payment.

### Scenario: Valid card

- GIVEN an authorised card
- THEN the payment is captured

## Requirement: Refund

The system MUST refund a charge.

### Scenario: Full refund

- THEN the charge is refunded

## Requirement: Dispute

The system MUST record a dispute.

### Scenario: Chargeback

- GIVEN a captured payment
- WHEN the bank opens a chargeback
- THEN a dispute is recorded

```markdown
### Requirement: Example inside a fence
#### Scenario: stays as written
```

## Notes

```text
## Requirement: Not a heading
### Scenario: Not one either
```

Trailing   prose.  
