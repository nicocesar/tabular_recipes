# 17. Dishes group variants; the server computes what differs

Date: 2026-11-25

## Status

Accepted

## Context

The chest collects several recipes for the same dish (three chicken curries).
A cook wants one entry per dish and to see at a glance what each variant
does differently.

## Decision

- **A Dish** has an id and a name per language. It has no language itself,
  so variants from Spanish and English sources sit together.
- **Grouping is the tabulator's:** the session is given the Dish list (all
  names) and `submit_recipe` names an existing Dish or proposes a new one,
  plus a **Label** (a few words: "with coconut milk & spinach") and a
  **canonical name** for every Ingredient — English, lowercase, singular
  ("chicken", "tomato"), a hidden matching key whatever the source language.
- **The Reference** is the first card filed into the Dish; the Owner can
  promote another. The rest are compared against it.
- **Variant numbers** are assigned at filing and never reused or
  renumbered: a printed "variant 3" stays true (ADR 0019). A card moved or
  merged into another Dish takes that Dish's next number; the Trace keeps
  the old one.
- **Chips are computed, not written:** added, changed (same canonical name,
  other item or amount) and missing ingredients against the Reference. More
  than three of a kind collapse ("− 4 spices"). Chip text is the card's own
  wording in the visitor's language when an Overlay exists, else the
  original with a language tag.
- **Home lists Dishes** with up to three Labels each; the Dish page lists
  Variants with Label and chips; a card opened from its Dish shows marks
  (new, "was …", and "Not in this variant:" in red), switchable off, never
  printed. Steps are never compared: two tabulations of the same text give
  different trees.
- **Owner tools:** move card, merge Dishes, promote Reference, edit Label
  and Dish name per language, unfold (ADR 0018).
- **Existing cards** get Dish, Label and canonical names from a one-time
  backfill Run each: the tabulator resubmits the same tree with the new
  fields, and the server rejects any change to the tree.

## Considered Options

- **Server matches on normalized titles.** Rejected: "Easy Weeknight Chicken
  Curry" ≠ "Chicken Curry".
- **Agent-written difference lines.** Rejected: every Reference change
  would re-pay a rewrite per variant per language, and prose can contradict
  the card.
- **Ingredient matrix as the default view.** Kept as a later "compare"
  link; too wide past three variants on a phone.
- **Numbers that follow the Reference.** Rejected: promoting renumbers every
  printout.
- **One Dish per language.** Rejected: hides translated variants.

## Consequences

- The recipe schema gains `dish`, `label`, `lang` and per-ingredient
  `canon`.
- The Dish page is `/dishes/{id}`; a one-card Dish redirects to its card.
  Its links open each Variant with `?marks=1`; the card page offers the
  same switch. Marks tint the SVG's ingredient cells (green new, amber
  changed, with "new" / "was …" after the words) and list the Reference's
  missing ingredients above the card. The download and the print never
  carry them.
- Labels in other languages wait for Overlays (ADR 0015); until then a
  Label shows in the card's language with a language tag.
