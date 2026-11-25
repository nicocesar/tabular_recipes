# 2. A recipe is a tree; the server lays it out

Date: 2026-11-25

## Status

Accepted

## Context

The "Cooking for Engineers" table puts each ingredient in a row of the left
column; each step is a cell to the right spanning the rows of everything it
combines, until the last step spans the whole table. Underneath it is a
tree: ingredients are leaves, steps are internal nodes, the finished dish is
the root. Row order, column positions and spans are all derivable from it.

## Decision

The stored recipe is the tree: `ingredients[]` (`id, qty, unit, item,
note`) and `steps[]` (`id, text, inputs[]`) plus title, credit, servings,
time and notes. Quantities stay strings, as the source wrote them.

The server validates every submission: unique ids, every ingredient used
exactly once, every step used exactly once except one root, no cycles, every
step has at least one input. The layout is computed, never stored: rows are
the leaves in depth-first input order (which makes every step's leaves
contiguous, so any valid tree draws without crossings); a step's column is
one more than its deepest input; each cell stretches right until its
consumer's column.

## Considered Options

- **The agent submits the grid (rows, columns, spans).** Rejected: layout in
  model output is fragile to validate and ties storage to one drawing.
- **Free markdown/HTML.** Rejected: cannot be validated or re-rendered for
  print and phone.
- **Numeric quantities for scaling.** Deferred: "a pinch" and "1 ½" stay
  honest as strings; scaling would be its own decision.

## Consequences

- The same tree renders to the screen card, print, and the text fallback.
- A rejection names exactly what is wrong, so the agent can fix it in-session.
