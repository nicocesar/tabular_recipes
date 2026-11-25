# 15. A translation is a text overlay on one tree

Date: 2026-11-25

## Status

Accepted

## Context

Only words change with language: the title, ingredient items, notes and the
wordy parts of quantities ("a pinch"), step text, Notes, the Label and the
Dish name. The tree — ids, inputs, order — does not.

## Decision

A card keeps **one tree in its source language** (its `lang`, written by the
tabulator). A Translation is an **Overlay**: a map from id to text
(`title`, `notes`, each ingredient's `qty`/`unit`/`item`/`note`, each step's
`text`, plus `label` and `dish`). The server lays out the original tree with
the Overlay's words. Validation: every id is covered, no id is added. Each
Overlay passes the Fit on its own and carries its own model, time and
Meters. The Credit says "translated from Spanish".

## Considered Options

- **A full Recipe per language.** Rejected: the server would have to prove
  every translation's tree identical to the original, and a helpful
  translator could merge two steps and drift.

## Consequences

- The translator cannot change structure, only words.
- Translations are small; storing several per card is cheap.
