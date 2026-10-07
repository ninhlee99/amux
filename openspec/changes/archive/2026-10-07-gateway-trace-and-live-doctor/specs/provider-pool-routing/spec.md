## ADDED Requirements

### Requirement: Healthy accounts first within a tier
Within one tier, the router SHALL try accounts whose health score is below 80 (degraded) only after the healthy accounts of that tier, keeping round-robin order among accounts of equal health. Quarantined and cooling accounts remain skipped.

#### Scenario: Flaky web account
- **WHEN** two web accounts are in the pool and one has dropped to health 70 after 5xx errors
- **THEN** new turns are served by the healthy account while it keeps succeeding
