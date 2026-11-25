# 9. One owner credential, no accounts (stage 1)

Date: 2026-11-25

## Status

Accepted

## Context

There are no users in stage 1, but someone must delete junk, retry failures
(paid runs), and see the admin page (last Tick, today's runs, spend) and each
record's trace. The stack's identity model (accounts, invites, API keys) is
far more than one person needs.

## Decision

- **Public:** list, recipe page, SVG, print, add (ADR 0008), `/healthz`, `/version`.
- **Owner:** delete, retry, `/admin`, `/admin/recipes/{id}` (trace). `/login`
  compares a passphrase with `OWNER_PASSWORD` in constant time and sets a
  signed `SESSION_SECRET` cookie lasting 180 days (`HttpOnly; Secure;
  SameSite=Lax`). Cookie-authenticated writes pass an Origin check.
- **`TICK_TOKEN`** admits only `/tick` and `/work/{id}`; nothing human uses it.
  An unset token never makes an empty Bearer valid.

## Considered Options

- **Full identity model now.** Rejected: no second person needs to write yet.
- **Admin via TICK_TOKEN.** Rejected: rule 22, one secret per caller.
- **Whole site behind login.** Rejected: kitchen devices and shared links
  should just work.

## Consequences

- Stage 2 lets anyone with a Google ID self-register to add recipes and keep
  history. That knowingly breaks the identity rule "Google sign-in never
  creates an account" and gets its own ADR then. The owner becomes a user
  with the admin flag; `OWNER_PASSWORD` is retired or kept as the
  break-glass way to appoint the first admin.
