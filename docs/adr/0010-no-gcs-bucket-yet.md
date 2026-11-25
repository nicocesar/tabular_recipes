# 10. No GCS bucket yet

Date: 2026-11-25

## Status

Accepted

## Context

Stack rule 4 pairs Datastore with a private GCS bucket named after the domain.
This project stores no files: no uploads, no photos, no copies of pages; the
SVG is generated from the tree in microseconds.

## Decision

Datastore only. Kinds: `Recipe` (indexed: Status, NormURL, CreatedAt; the
rest as an unindexed JSON body) and `Counter` (daily caps, keyed by date and
date+IP hash). SVG and print are rendered per request with an ETag from the
record's version.

## Considered Options

- **Create the bucket now for later.** Rejected: an unused resource with IAM
  to maintain, inviting someone to cache SVGs there for no gain.
- **Cache rendered SVGs in a bucket.** Rejected: rendering is cheaper than
  the round trip.

## Consequences

- Rule 5 (signed URLs) doesn't apply. When photos arrive (stage 2), the bucket
  `recipes.nicocesar.com` is created then, with its own ADR.
