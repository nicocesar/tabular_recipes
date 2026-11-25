# 6. The agent fetches pages with web_fetch, under unrestricted networking

Date: 2026-11-25

## Status

Accepted

## Context

Someone must read the recipe page. The server could fetch it and hand the
text over, or the agent could fetch it.

## Decision

The agent fetches with the built-in `web_fetch` tool
(`max_content_tokens` ≈ 50k). Every other sandbox tool (bash, files,
web_search) is disabled. The environment uses **unrestricted** networking:
`limited` networking also restricts `web_fetch` to an allow-list of hosts,
and recipes come from any site; with no sandbox tools enabled nothing in the
container can use the open network.

When a site blocks the fetch, the form's "paste the recipe" box is the
fallback: pasted text goes into the first message and the agent doesn't fetch.

## Considered Options

- **The server fetches and strips HTML.** Rejected by the owner; also means
  writing readability/JSON-LD extraction ourselves.
- **web_search too.** Rejected: the job has one URL; search adds spend.

## Consequences

- The server never sees the raw page; the trace records the agent's account
  (fetch count from session usage, failure reasons via `report_failure`).
- No fetched page is stored (ADR 0012).
