## ADDED Requirements

### Requirement: Tool arguments match their schema types
Before a web model's tool call reaches the client, arguments whose schema type is array, integer/number or boolean SHALL be converted from the string forms web models produce (comma lists or JSON-array strings, numeric strings, "true"/"false").

#### Scenario: WebSearch allowed_domains as a string
- **WHEN** a web model calls WebSearch with `"allowed_domains": "iana.org"`
- **THEN** the client receives `"allowed_domains": ["iana.org"]` and the call validates
