# 14. Translation is a Managed Agent (breaks nico-stack rule 15)

Date: 2026-11-25

## Status

Accepted

## Context

Visitors read the chest in their own language (ADR 0016), so a card is
translated on demand. nico-stack rule 15 says small single-shot jobs skip
agents: one Messages API call with structured output. Translating a card's
text looks like one, but it is not single-shot: the translation must pass
the one-page Fit in the target language (ADR 0004), and a long card in a
wordier language is rejected and has to be shortened. That is the
submit → reject → fix loop the tabulator's session already runs.

## Decision

A second agent, **`recipes-translator`**, declared in code beside the
tabulator and upserted on boot (rule 14). Its only tool is the custom
`submit_translation`, whose input is an Overlay (ADR 0015). It has no
`web_fetch` and no toolset. The model is `TranslatorModel =
"claude-sonnet-5-5"`, a constant of its own (regional cooking terms —
zapallo, choclo, a punto nieve — are where a cheaper model's mistakes cost
a dinner).

A Translation is a Run of a second kind on the same path: self-dispatch,
Lease, Tick repair, Trace, Meters, the $0.50 session budget. It counts
against the shared daily caps (100 overall, 25 per Visitor) and is deduped
per (card, language) through the same Claim as filing.

## Considered Options

- **Messages API with structured output (rule 15 as written).** Rejected:
  needs a second client, a second retry loop for fit rejections, a second
  meter shape and its own stall handling in the Tick.
- **Haiku 5.5.** Deferred: the input is small, so the saving is a cent or
  two per card. Revisit if Meters show translations are a real share.

## Consequences

- Monthly cost of the break: none beyond tokens. Sessions bill by use and
  nothing stays warm (rules 6–8 untouched).
- A stuck translation is repaired exactly like a stuck tabulation.
