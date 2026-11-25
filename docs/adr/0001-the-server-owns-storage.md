# 1. The server owns storage

Date: 2026-11-25

## Status

Accepted

## Context

Recipes are written by one process today (the run that tabulates a page),
but stage 2 adds accounts and history, and scripts or other services may
want to add recipes later. If anything other than the server writes to
Datastore directly, the invariants the server enforces — a tree that
validates, one page that fits, the daily caps — can be bypassed.

## Decision

Storage sits behind one Go interface (`internal/store`) owned by the server.
Production is Firestore in Datastore mode; development is a filesystem
implementation of the same interface, so `make run` needs no cloud
credentials. Queries are equality filters on single properties, sorted in
memory — no composite indexes. Anything outside the server goes through its
HTTP API.

## Considered Options

- **Scripts write Datastore directly.** Rejected: duplicates validation and
  needs GCP credentials outside the service.
- **SQL (Cloud SQL / SQLite on a volume).** Rejected: an always-on database
  or a mounted disk breaks scale-to-zero and costs money while idle.

## Consequences

- One place validates a recipe. Tests run against the filesystem store.
- Listing sorts in memory; fine for thousands of records, revisit beyond.
