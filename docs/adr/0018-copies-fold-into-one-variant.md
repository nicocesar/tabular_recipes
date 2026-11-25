# 18. Copies fold into one variant

Date: 2026-11-25

## Status

Accepted

## Context

The same recipe is published on many sites — copies, aggregators, SEO
farms. On day one the chest had One Bowl Buttermilk Bran Muffins from
thecafesucrefarine.com and getrecipecart.com: 15 of 15 ingredients and
amounts identical, but two different step trees, because the tabulator does
not tree the same text the same way twice.

## Decision

After a card is filed into a Dish, the server compares it with every
variant. It is **the same variant** when:

1. its canonical ingredient set differs by at most one ingredient, and
2. on shared ingredients, quantity and unit match after normalising
   (`1 ½` = `1½` = `1 1/2`; `tsp` = `teaspoon`, `Tbsp` = `tablespoon`, …)
   with at most one mismatch.

Steps, notes, order and title are ignored. A match **folds** the card: the
variant keeps its tree and number, the new source URL joins its **Also
found at** list (screen and print), and the new record becomes `folded`
with a pointer to the variant. Its Claim stays, so that URL opens the
variant. The Trace records the comparison ("15/15 ingredients, 15/15
amounts"). The Owner can **unfold** a card into its own variant.

## Considered Options

- **Keep it as a variant labelled "same ingredients".** Rejected by the
  owner: copies are common enough that the chest would fill with them.
- **Let the tabulator report duplicates.** Rejected: the run is already
  spent by then, and the data judges better than the agent.
- **Compare trees.** Rejected: unstable for the same text.

## Consequences

- A cups-to-grams conversion of a recipe stays a separate variant.
