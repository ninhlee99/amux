## ADDED Requirements

### Requirement: Web tool inputs are JSON objects
Every tool call parsed from web text SHALL reach the client with a JSON-object input. A bare value SHALL become `{"<key>": value}` when the tool's schema has exactly one required string parameter, and `{}` when it has no required parameters; any other non-object input MUST drop the call rather than send an input the IDE rejects. Gemini's native `call:default_api:<tool>{Key: "v"}` syntax SHALL keep its keys.

#### Scenario: ReAct bare input
- **WHEN** a web model answers `Action: Bash` / `Input: ls -la`
- **THEN** the client receives Bash with input `{"command":"ls -la"}`

#### Scenario: Bare value for a multi-parameter tool
- **WHEN** a web model answers `Action: Edit` / `Input: main.go`
- **THEN** no Edit call is sent

#### Scenario: Gemini native call
- **WHEN** a web model writes `call:default_api:view_file{AbsolutePath: "/w/main.go"}`
- **THEN** the client receives view_file with input `{"AbsolutePath":"/w/main.go"}`
