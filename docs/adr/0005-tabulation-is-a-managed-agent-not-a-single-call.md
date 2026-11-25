# 5. Tabulation is a Managed Agent, not a single call

Date: 2026-11-25

## Status

Accepted

## Context

Stack rule 15 says small, single-shot extraction skips agents and uses one
Messages API call. "Turn this page into JSON" looks like extraction.

## Decision

Tabulation runs as a Claude Managed Agent (`recipes-tabulator`), declared in
code and upserted on boot. Rule 15 does not apply because:

- it is not one shot: the validator (ADR 0002, 0004) rejects submissions and
  the agent fixes them inside the same session;
- restructuring prose ("meanwhile brown the butter… then combine") into a
  tree is judgment, not lookup;
- the agent fetches the page itself (ADR 0006).

Its result comes back through the custom tool `submit_recipe`, whose input
schema is the tree; `report_failure` lets it decline honestly
(`fetch_blocked`, `not_a_recipe`, `ambiguous`). A session that goes idle
with neither, after one nudge, is marked Failed with "no submission".

## Considered Options

- **One Messages API call with structured output.** Rejected: the server
  would have to hand-roll the rejection loop and the fetch.
- **Messages API + Tool Runner hosted by us.** Rejected: we would host the
  loop inside the request anyway, with no gain over a hosted session.

## Consequences

- Each run costs session runtime on top of tokens; bounded by ADR 0008.
- Every run has a Console trace URL, linked from the record's trace.
