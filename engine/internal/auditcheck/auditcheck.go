// Package auditcheck names the checks the auditor runs, so render can hold
// vocabulary.yaml's audit_rules to them without importing audit.
package auditcheck

// Names are the checks audit runs. `audit_rules` must name exactly these.
var Names = []string{"refusal-bypass", "isolation-bypass", "delivered-untested", "edit-before-red", "test-weakened", "checker-wrote", "scout-wrote", "archive-without-red"}
