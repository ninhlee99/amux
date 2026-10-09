## ADDED Requirements

### Requirement: ChatGPT python tool runs on the client
When the client sent tools and ChatGPT Web addresses code to its own `python` tool, the adapter SHALL return a Bash tool call that runs that code with `python3` on the client instead of ChatGPT's tool error or canned "can't do more advanced data analysis" reply.

#### Scenario: Python tool unavailable on the account
- **WHEN** ChatGPT streams a `code` message to `python`, then a `system_error`, then "It seems like I can’t do more advanced data analysis right now."
- **THEN** the client receives one Bash tool call running the code and none of that text
