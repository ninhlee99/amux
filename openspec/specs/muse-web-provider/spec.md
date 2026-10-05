# muse-web-provider Specification

## Purpose
Use Meta Muse, which has no public API, by driving its web app in amux's own logged-in browser profile — as a gateway pool backend and through MCP tools.

## Requirements
### Requirement: Browser-driven Muse session
amux SHALL reach Meta Muse (`https://muse.ai`) by driving its web app in a dedicated Chromium profile (`~/.amux/browser-profiles/muse`, overridable with `AMUX_MUSE_PROFILE`) over the DevTools Protocol, or by attaching to an existing browser given in `AMUX_MUSE_CDP`. A browser amux attached to MUST never be killed; one amux launched is shared by other amux processes through `DevToolsActivePort`, each using its own tab.

#### Scenario: Gateway and MCP at the same time
- **WHEN** the gateway already launched the Muse browser and `amux mcp` calls `muse_chat`
- **THEN** the MCP process attaches to the same browser in a new tab instead of failing on the profile lock

### Requirement: Login without keychain
`amux login muse` SHALL open the Muse profile visibly, wait (up to 5 minutes) until `POST /api/auth/check` from the page reports `ok`, and save a `muse_web` account (`muse:web:NN`, web tier, priority 27) with the viewer id; no cookie is copied out of the profile and no keychain is read.

#### Scenario: Successful login
- **WHEN** the user signs in to Meta in the opened window
- **THEN** `accounts.json` gains `muse:web:01` and `amux account list` shows it

### Requirement: Faithful chat turns
A chat turn SHALL send the prompt verbatim (no role-play markers), attach files through the composer's file input (local paths, `file://`, `http(s)://`, `data:`), detect completion from the stop button and text stability, and report errors shown by the app. Streamed text MUST only grow by appending stable text, never re-emitting a rewritten draft.

#### Scenario: Draft rewritten mid-answer
- **WHEN** Muse first renders "Thinking" and then replaces it with the answer
- **THEN** the client never receives "Thinking" followed by the answer as appended text

### Requirement: Pool backend behaviour
The `muse_web` adapter SHALL run `FullContext` requests on a new side chat, reuse a per-project thread otherwise, apply web tool-call emulation, and return authentication or rate-limit failures that happen before any output synchronously as `ErrAuthentication` / `ErrRateLimitReached`.

#### Scenario: Signed out
- **WHEN** the Muse profile is signed out and a request arrives
- **THEN** the adapter fails with `ErrAuthentication` and the pool fails over to the next account

### Requirement: Chats and media
amux SHALL list sidebar chats, open a chat by index, title, thread URL or id, read messages with media links, and download generated media from inside the page (handling `blob:` and cookie-authenticated URLs) to `~/.amux/muse/media` or a given directory.

#### Scenario: Download generated video
- **WHEN** `muse_media` is called with `download: true` on a chat containing an `.mp4` link
- **THEN** the file is saved locally and its path, size and MIME type are returned

