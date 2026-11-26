# 19. The print carries the variant, a short link and a QR code

Date: 2026-11-25

## Status

Accepted

## Context

A printed card has to lead back to the right card online. Variant numbers
stay fixed (ADR 0017) but a card can still move to another Dish or be
merged.

## Decision

The print header reads **Dish · variant N · Label** (no number when the
Dish has one card). The footer carries **Also found at** (ADR 0018), the
short link `recipes.nicocesar.com/r/{id}` and a QR code of it. The card id
never changes, so the link survives moves and merges. `/r/{id}` matches ids
case-insensitively so the QR can use uppercase alphanumeric mode: 45
characters fit a version 3 code (29×29 modules, level M), about 18 mm wide
with its quiet zone. The QR is print-only.

The encoder is **`rsc.io/qr`**: small, frozen, no transitive dependencies —
a deliberate addition under rule 1.

## Considered Options

- **Number only.** Rejected: breaks on move and merge.
- **Hand-written QR encoder.** Rejected: hundreds of fiddly lines.
- **Short link without QR.** Fine, but the owner wants the scan.

## Consequences

- Paper never depends on the Reference: marks and chips are screen-only.
- The QR sits in the title block's top-right corner, so it costs width,
  not height; the header line, "Also found at" (at most four sites, then
  "+N more") and the short link (after the imprint) add a few lines. The
  Fit checked at submission lays out a stand-in for all of it, so a card
  that passes still prints at 8 pt or more with its real furniture. The
  link and QR come from the request's host (rule 17).
