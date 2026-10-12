## ADDED Requirements

### Requirement: Gateway processes only remove their own pid files
The gateway daemon and supervisor SHALL remove gateway.pid / gateway.supervisor.pid on exit only if the file still names their own pid, and a supervisor SHALL NOT start while the recorded supervisor is alive.

#### Scenario: second start while running
- **WHEN** a second supervisor is launched while one is running
- **THEN** it exits without touching the pid files, and `amux status` / `amux stop` still find the running gateway
