## ADDED Requirements

### Requirement: Interactive TUI Dashboard
The system SHALL provide an interactive Terminal User Interface (`amux dashboard`) displaying real-time gateway status, active pool accounts, health latency, and 5h/7d quota metrics.

#### Scenario: Launching dashboard
- **WHEN** user executes `amux dashboard`
- **THEN** an interactive full-screen or inline terminal view is rendered displaying live gateway metrics and provider statuses

#### Scenario: Toggling account in pool via TUI
- **WHEN** user navigates to an account and presses Spacebar or Enter
- **THEN** the account's pool membership status is toggled and saved immediately without terminating the TUI
