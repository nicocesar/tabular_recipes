# 7. Start the tabulator on Claude Sonnet 5.5

Date: 2026-11-25

## Status

Accepted

## Context

The default model is Claude Opus 5.5 ($4/$20 per MTok). Claude Sonnet 5.5
is half that ($2/$10). The owner chose to start with Sonnet and observe.

## Decision

`recipes-tabulator` runs `claude-sonnet-5-5` at its default effort. The
model is one field in the agent registry; changing it creates a new agent
version on the next boot. Meters (ADR 0008) are recorded per record either
way, so the two can be compared on real runs.

## Considered Options

- **Opus 5.5 at high effort.** Deferred, not rejected: switch if Sonnet's
  trees need too many rejections or read poorly.

## Consequences

- If quality is the problem, the fix is one line plus a deploy.
