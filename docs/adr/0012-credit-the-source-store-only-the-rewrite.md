# 12. Credit the source; store only the rewrite; noindex

Date: 2026-11-25

## Status

Accepted

## Context

The site republishes other people's recipes publicly, from a public repo.
Ingredient lists are generally not copyrightable in the US; recipe prose,
headnotes and photos are.

## Decision

- Every card, print view and standalone SVG carries "Adapted from *title* —
  domain" (and the author's name when the page states it). Credit travels
  inside the SVG.
- Only the structured result is stored: no copy of the fetched page, no
  photos, no headnotes. The prompt tells the agent to rewrite steps in its own
  terse words and paraphrase notes.
- Pasted text has no URL: it is credited "pasted by a visitor" plus an
  optional source field on the form.
- The whole PoC is `noindex` (`X-Robots-Tag` and meta tag). Revisit at stage 2.

## Considered Options

- **Verbatim text and images.** Rejected: copyright, and not the format.
- **Indexable now.** Rejected: nothing gained during a PoC; SEO is hard to undo.

## Consequences

- A takedown is the owner deleting the record.
