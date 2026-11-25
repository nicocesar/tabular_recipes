# 4. One Letter page is enforced at submission

Date: 2026-11-25

## Status

Accepted

## Context

The printed card must fit one US Letter page and stay readable at the stove.
Scaling to fit no matter what would print a big recipe at 4 pt.

## Decision

After laying out a submitted tree, the server computes the scale needed to
fit the card in the printable area (0.4 in margins) in the better of portrait
and landscape. If cell text would print below **8 pt**, the submission is
rejected with a specific message (how many times too big, which steps are
longest) and the agent shortens it in the same session. After **3
consecutive fit-only rejections** the server accepts the recipe and marks it
"may not fit one page" in its trace and on the print view.

The print view picks orientation per recipe with `@page { size: letter
portrait|landscape }`.

**Before rejecting, the server narrows columns.** A deep tree (many steps
in sequence) has many step columns, and with fixed column widths the first
real run (a 12-step locro, 7 steps deep) was rejected at 6.5 pt, so the
agent merged 12 steps into 7 to pass. Now layout tries a short ladder of
column-width caps (step columns 140 → 62 pt, ingredients 190 → 108 pt)
and keeps the widest whose printed size is within 3% of the best
achievable: text wraps onto more lines, trading width for height. The same
widths are used on screen, so screen and paper match. The locro's first
submission now fits at 8.6 pt, unmerged. The agent is only asked to shorten
when no width fits.

## Considered Options

- **Always scale to fit.** Rejected: unreadable output.
- **Multi-page print.** Rejected: the owner wants one page.
- **Hard-fail after N fit rejections.** Rejected: loses a good recipe over
  layout.

## Consequences

- The prompt asks for terse, book-style step text from the start.
- Notes count toward the fit; they are the overflow valve, in smaller type.
