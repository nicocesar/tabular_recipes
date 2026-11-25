# 16. Page text comes from committed catalogs; the visitor picks a language

Date: 2026-11-25

## Status

Accepted

## Context

The chest's chrome (forms, errors, status lines, the card's fixed labels such
as "Serves") was English in code. Visitors are English and Spanish speakers,
and more languages will come through card translation (ADR 0015).

## Decision

**`internal/i18n`**: one embedded JSON file per language (`en.json`,
`es.json`), key → format string. `T(lang, key, args…)` falls back to
English. Templates call `{{$.T "key"}}` through the page view. A test checks
every language has every key with the same `%` verbs. Dates use catalog
month names. No plurals yet: texts are phrased to avoid them.

The card's fixed labels follow the **card's** language (the original or the
Overlay shown); the page chrome follows the **visitor's**. Text the agent
reads (fit messages, prompts) and `/admin` stay English.

The language is chosen by: `?lang=` on a card link (one view), else the
`lang` cookie set by the footer switch, else `Accept-Language`, else `en`.
Stage 2 moves the choice onto the user record.

A new page language is a reviewed commit, not an agent output: Grampa's
voice is part of the design (ADR 0011). A card can be shown in a language
the page chrome lacks; the chrome then falls back to English.

## Considered Options

- **`golang.org/x/text/message` + gotext.** Rejected: a dependency and an
  extraction pipeline for ~50 strings without plurals (rule 1).
- **Agent-translated catalogs stored in Datastore.** Rejected: unreviewed
  copy shown to every visitor.
- **Language in the URL (`/es/…`).** Rejected: pays off for search
  engines, and the chest is noindex (ADR 0012).

## Consequences

- Adding Italian page text is one file plus the test.
