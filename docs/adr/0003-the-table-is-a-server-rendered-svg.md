# 3. The table is a server-rendered SVG, with text drawn as glyph outlines

Date: 2026-11-25

## Status

Accepted

## Context

Wide tables don't fit a phone in portrait. The owner prefers pinch-zooming
an image of the whole card to scrolling a table. An SVG stays sharp at any
zoom; a PNG blurs. But an SVG has no text layout: the server must measure
text to wrap it inside cells, and the browser must then draw it with
exactly the metrics the server measured.

## Decision

`internal/card` lays out the tree and writes SVG. Text is measured with
`golang.org/x/image/font/sfnt` against fonts embedded in the package, and
drawn as **glyph outlines** (one `<path>` per distinct glyph in `<defs>`,
placed with `<use>`), so what was measured is exactly what is drawn — in a
browser, as a standalone file, in print — with no font loading at all.

The page inlines the SVG at fit-to-width. `GET /recipes/{id}.svg` serves the
same document standalone. Because outlines are not selectable text, every
recipe page also carries a plain-text rendering (ingredients and steps as
lists) for search, screen readers and copy-paste, generated from the tree.

`golang.org/x/image` is a deliberate addition to rule 1's "few
dependencies": it is the Go project's own extended library and the only way
to read font metrics without cgo.

## Considered Options

- **HTML `<table>` with `rowspan` and a sticky column.** Rejected by the
  owner: pinch-zoom of the page is worse than of an image.
- **SVG `<text>` with web fonts.** Rejected: an SVG loaded as an image or
  downloaded can't fetch fonts, and any metric drift breaks wrapping.
- **PNG.** Rejected: blurs when zoomed, needs a rasteriser.
- **`<foreignObject>` with HTML.** Rejected: inconsistent in browsers and print.

## Consequences

- No kerning (the sfnt package reads no GPOS); acceptable at these sizes.
- SVG size grows with text length (one `<use>` per glyph): tens of KB.
- Changing a font means only replacing a file in `internal/card/fonts`.
