## Why

During `/open-pr:fix` through ChatGPT Web every model turn sat ~16–17 minutes before `POST /backend-api/sentinel/chat-requirements` failed with `read tcp … read: operation timed out`. Pooled HTTP/2 connections dropped silently (IPv6/NAT) kept taking requests, each waiting out the OS TCP timeout; the 5-minute response-header bound never applied.

## What Changes

- Web transports (shared default and egress-proxy) send HTTP/2 pings after 30s idle and drop a connection whose ping is unanswered in 15s.
- The ChatGPT sentinel call has its own 60s deadline.

## Capabilities

### Modified Capabilities
- `web-providers`: connection health checks.

## Impact

- `pkg/provider/http_client.go`, `pkg/provider/chatgpt_sentinel.go`, `pkg/guard/proxy_egress.go`.
