## ADDED Requirements

### Requirement: A busy gateway is not mistaken for a stopped one
The CLI liveness check SHALL treat a gateway whose port accepts connections but answers the status call slowly as running, and SHALL NOT start a second gateway or rewrite gateway.pid in that case. The status endpoint SHALL answer within the CLI probe budget when idle.

#### Scenario: four sessions streaming
- **WHEN** `amux run claude` starts while four sessions are streaming through the gateway
- **THEN** it uses the running gateway and `amux status` still reports the daemon's pid
