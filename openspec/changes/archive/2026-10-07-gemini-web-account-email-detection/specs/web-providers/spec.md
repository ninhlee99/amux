## ADDED Requirements

### Requirement: Gemini web account identity detection
Gemini web login SHALL extract the user's account email and subscription tier from the authenticated session at `gemini.google.com/app`.

#### Scenario: Gemini web login email detection
- **WHEN** the user completes login to Gemini Web
- **THEN** amux loads `https://gemini.google.com/app` with the session cookies, extracts the user's email, and saves the account under `gemini:web:<email_prefix>`
