# GRAPH — amux

> Neural map · 21 modules · 97 files · 1129 funcs · 0 LLM tokens  
> docs/GRAPH.md (portable · root=.)  
> AI: đọc **mesh + hubs + 1 subnet** — cấm dump toàn bộ. Chi tiết 1 func: `am map get`.

## Mesh (module → module)

```
auth → types
bridge → guard monitor privacy router tools types usage
cli → env hook monitor nav privacy profile provider proxy types ui usage
env → types
guard → types
hook → types
monitor → term types
privacy → monitor types
profile → auth types
provider → auth browser guard profile tools types
proxy → auth bridge guard hook monitor nav privacy profile provider router term types usage
router → guard privacy term types
tools → monitor types utils
ui → browser guard hook profile provider proxy router term types usage
usage → nav types
```

## Neurons (hubs)

| module | files | funcs | hubs |
|--------|------:|------:|------|
| `auth` | 3 | 16 | KCAccount, KCGet, KCSet, RefreshClaudeToken, RefreshLiveClaudeToken, RefreshedCredsJSON |
| `bridge` | 6 | 62 | EstimateBytesTokens, EstimateInputTokens, EstimateStringTokens, HandleClaudeCountTokens, HandleClaudeMessages, ToChatRequest |
| `browser` | 3 | 32 | CaptureCookieViaBrowser, CaptureWebAuthViaBrowser, RefreshWebAuthFromProfile, BrowserInfo, CapturedWebAuth, ChatGPTSession |
| `cli` | 3 | 53 | Run, accountRef, accountToggleHint, applyAccountEnabled, bytesTrim, cmdAdd |
| `env` | 1 | 5 | EnvPath, LoadEnvVars, PrintEnvExports, SaveEnvVars, ShellQuote |
| `guard` | 6 | 54 | Unpin, ActivePinsCount, ExtractSessionKey, GetPinned, NewSessionAffinity, Pin |
| `hook` | 5 | 62 | AutoUpdateConfigFile, IsAutoUpdateEnabled, LaunchAgentPath, SetupAutoUpdate, AddHook, AppendLine |
| `live` | 0 | 0 |  |
| `monitor` | 3 | 35 | GetLogStats, GetRequestMetrics, ResetRequestMetrics, ResetStats, SetDiagnosticAllBodies, SetLogAllBodies |
| `nav` | 8 | 100 | GitRoot, LearnFuncs, Resolve, WorkspaceDir, ApplyAnnotations, GenerateMap |
| `privacy` | 1 | 20 | RedactString, Kinds, LogHits, MergeResults, RedactBytes, RedactChatRequest |
| `profile` | 2 | 62 | ActivePath, ApplyEntry, AutoBackup, BundlePath, CmdSave, CmdUse |
| `provider` | 15 | 187 | AGYAuthAvailable, AGYCredentialsPath, Group, Priority, SendMessageStream, SupportsTools |
| `proxy` | 11 | 123 | ComposeListenAddr, DialAddr, IsPublicListen, ListenAddr, ListenAddrPath, LoadListenAddr |
| `router` | 5 | 53 | GroupIndex, DetermineAdapterGroup, GroupDisplayName, GroupPriorityForIDE, IDEFromClientDialect, NativeGroups |
| `term` | 2 | 64 | CyanErr, DimErr, GreenErr, Log, LogAuth, LogDegraded |
| `tools` | 7 | 62 | FromClaudeToolUseBlocks, ParseClaudeTools, ToClaudeToolUseBlocks, ToClaudeTools, ParseCodexResponsesTools, ToCodexResponsesTools |
| `types` | 6 | 35 | AccountBrandID, AccountBrandIDWithDomain, AccountNamedID, AccountNamedIDWithDomain, EmailDomainPart, EmailLocalPart |
| `ui` | 5 | 74 | CmdDoctorProviders, CmdAPI, CmdAccounts, CmdAccountsCmd, CmdAccountsFilter, CmdLogin |
| `usage` | 4 | 29 | ProjectForRemoteAddr, AppendUsageEntry, ProjectLabel, UsageLogPath, WrapUsageCapture, Close |
| `utils` | 1 | 1 | NormalizeJSONSchema |


## Subnets

Không nhúng hết vào đây (tiết kiệm token). Lấy 1 subnet:

```bash
am map graph <module>     # ví dụ: am map graph nav
am map graph --list
```
