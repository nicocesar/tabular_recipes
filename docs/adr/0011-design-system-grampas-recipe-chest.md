# 11. Design system: Grampa the Great Engineer's recipe chest

Date: 2026-11-25

## Status

Accepted

## Context

The SVG card is the product, on screen, in the kitchen and on paper. It needs
a deliberate personality (stack rule 24), not a template look.

## Decision

A wooden chest of typewritten index cards found in an attic, laid out with an
engineer's precision.

- **Metaphors:** the home page is the chest (cards' top edges, newest first);
  each recipe is a card with a faded red header rule and pale blue ruled lines;
  the add form is "File a new recipe"; the footer imprint reads *From the chest
  of Grampa the Great Engineer*.
- **Type:** Special Elite (titles, wordmark), Source Serif 4 Small Text (cells),
  Courier Prime (quantities, and any token with a digit: times, temperatures).
  All open-licensed, embedded, self-hosted.
- **Palette:** card `#F2E8D0`, ink `#3A2E26`, index red `#B0473A`, blueprint
  blue `#6F8FB5`, pencil `#8A8278`. Dark mode ("lamp-lit workshop"): walnut
  `#231C17`, card-coloured ink, amber `#D9A441`.
- **Stains:** two marks (three on a well-used card) (coffee ring, grease halo, flour
  fingerprints, tomato splash, pencil tick) at roughly 15–30% opacity (first set at 6–12%, which proved invisible on the card stock), seeded from the
  recipe id so a card keeps its history, placed only in margins and never over
  text, aria-hidden, dropped in print, near-invisible in dark mode. A card
  viewed often earns one more, up to the cap (a view counter on the record,
  counting full page loads only).
- **Print:** paper tint, ruled lines and stains dropped; ink stays.

## Considered Options

- **Clean "engineering drawing / index card".** Rejected by the owner as too
  crisp; kept as the table's inner discipline.
- **Food-blog warmth (scripts, photos).** Rejected: fights the tabular idea.
- **Default framework look.** Rejected: rule 24.

## Consequences

- Fonts live in `internal/card/fonts` (used for outlines) and are served to
  the HTML chrome from the same embedded FS.
