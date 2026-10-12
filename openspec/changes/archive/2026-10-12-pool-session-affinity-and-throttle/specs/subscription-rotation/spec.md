## ADDED Requirements

### Requirement: Snapshotted profiles respect turned-off identities
The Claude rotator SHALL treat a saved profile as off when the subscription identity with the same account email is turned off, including profiles snapshotted from the live login.

#### Scenario: fresh AMUX_HOME
- **WHEN** the gateway snapshots the live Claude Code login of an identity the user turned off
- **THEN** requests do not use that subscription
