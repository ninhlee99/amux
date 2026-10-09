## ADDED Requirements

### Requirement: Dead web connections are detected quickly
Web provider transports SHALL health-check idle HTTP/2 connections with pings so a silently dropped connection is discarded within about 45 seconds, and the ChatGPT sentinel request SHALL fail after 60 seconds instead of waiting for the operating system's TCP timeout.

#### Scenario: Pooled connection dropped by the network
- **WHEN** the HTTP/2 connection to chatgpt.com stops answering while idle
- **THEN** the next request uses a new connection rather than hanging for many minutes
