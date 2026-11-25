# 8. An open add form for the PoC, with layered spend caps

Date: 2026-11-25

## Status

Accepted

## Context

For the proof of concept the owner wants anyone with the link (friends) to
add recipes, with no accounts. `POST /recipes` starts a paid agent run, and
the service is public at the IAM layer, so an open form lets any visitor or
bot spend the Anthropic budget.

## Decision

Adding is open, behind layers from cheapest to hardest:

1. **Dedupe by URL** — a URL already Ready redirects to it, at no cost.
2. **Honeypot field** — a hidden input bots fill; such posts are dropped.
3. **Per-IP cap** — 5 runs/day per hashed client IP (soft; IPs are cheap).
4. **Global cap** — 25 runs/day across everyone, counted in the store.
   Resumes count too.
5. **Session budget** — each session is created with a hard $0.50 list-cost cap.
6. **Workspace spend limit** — the project's own Anthropic workspace has a
   monthly limit set in the Console (≈$20), which holds even if every guard
   above has a bug.

Cost per run is to be filled in here from real meters after the first runs.

## Considered Options

- **Owner-only adding.** Rejected for the PoC: friends should try it.
- **CAPTCHA / Turnstile.** Rejected: third-party JS on a no-JS form;
  reconsider if bots appear.
- **No caps.** Rejected: unbounded bill.

## Consequences

- Worst case per day ≈ 25 × $0.50; per month bounded by the workspace limit.
- Stage 2 (Google accounts) replaces the per-IP cap with a per-user one.
