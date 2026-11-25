# 13. A run is a self-dispatched request; the Tick repairs

Date: 2026-11-25

## Status

Accepted

## Context

A run takes 1–3 minutes. The service bills request-based CPU
(`--cpu-throttling`) and scales to zero, so a goroutine that outlives its
request stalls. Holding the browser's own POST open ties the run to a phone
that may sleep.

## Decision

- `POST /recipes` saves the record as **Dispatched**, fires
  `POST {WORK_URL}/work/{id}` (Bearer `TICK_TOKEN`), and answers 303 to the
  recipe page.
- `/work/{id}` **is** the run. It takes a lease (`LeaseUntil` = 10 min ceiling
  + 2 min slack; 409 while live), marks **Running**, creates the session and
  **saves its id immediately** (the checkpoint), streams to completion,
  validates submissions and answers each tool call, stores the result as
  **Ready** or **Failed**, and clears the lease. Service timeout: 15 min.
- A resume reattaches to the saved session instead of creating a new one, so
  finished agent work is never paid twice. A retry of a Failed record (owner
  only) starts a new session.
- A failed dispatch is never run locally; the record waits for the Tick.
  Without `WORK_URL` (a laptop), runs are goroutines.
- The recipe page polls every 3 s with htmx while Dispatched/Running.
- **`POST /tick`** (Cloud Scheduler, every 15 min, `TICK_TOKEN`) re-dispatches
  records Dispatched for over 2 min and Running with an expired lease, at most
  3 per pass; after 3 resumes a record is Failed ("gave up after 3 resumes").
  It always answers 200 with errors in the body. The same pass runs on boot.
  `/admin` shows the last pass and warns on "never".

## Considered Options

- **Always-on CPU with background workers.** Rejected: the $45/month lesson.
- **Cloud Tasks / Cloud Run Jobs.** Rejected (stack rule 7).
- **SSE progress.** Rejected (rule 8): holds an instance open.

## Consequences

- No `--no-cpu-throttling` and no min instances, ever, for this design.
