## ADDED Requirements

### Requirement: Email redaction keeps file names and no-reply addresses
Outbound privacy redaction SHALL NOT replace an email-like match whose final domain label is a file extension (for example `page@3f9a1c2e.webm`, `logo@2x.png`), or a no-reply address (`noreply@…`, `no-reply@…`, `…@users.noreply.github.com`). Other email addresses SHALL still be replaced with the sample.

#### Scenario: Playwright capture file name
- **WHEN** a tool result contains `mv /tmp/out/page@3f9a1c2e.webm dashboard.webm`
- **THEN** the text sent upstream is unchanged
