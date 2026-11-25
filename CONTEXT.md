# Tabular Recipes (recipes.nicocesar.com)

A public chest of recipe cards. Anyone with the link files a recipe by URL
(or pasted text); a Claude Managed Agent rewrites it as a tree in the
"Cooking for Engineers" tabular style; the server lays the tree out as a
card — an SVG table — for the kitchen screen and for one printed Letter page.

## Language

### The recipe

**Recipe**:
One way to cook a Dish, as stored: a title, a Credit, servings and time,
Ingredients, Steps and Notes, in its source language. Its shape is a Tree.
Shown as one Variant of its Dish.
_Avoid_: entry, post, item

**Ingredient**:
A leaf of the Tree: quantity, unit, item and note, exactly as the source
wrote them (strings, never converted). Each is consumed by exactly one Step.
_Avoid_: component, input (an input may also be a Step)

**Step**:
An internal node of the Tree: a short verb phrase ("cream", "bake 25 min
350°F") and its inputs (Ingredients or earlier Steps). Drawn as a cell
spanning the rows of everything it combines.
_Avoid_: instruction, action, operation

**Root**:
The one Step no other Step consumes — the finished dish. It spans every row.

**Tree**:
The Recipe's structure: Ingredients as leaves, Steps as nodes, one Root.
What the agent submits and what is stored; the table is derived from it.
_Avoid_: graph, DAG, grid

**Notes**:
Short paraphrased remarks that don't belong in a cell (why the dough rests,
substitutions). Printed below the table, in smaller type.

**Credit**:
Where the Recipe came from: source title, domain, link and author when
known; "pasted by a visitor" for pasted text.
_Avoid_: attribution, citation

### The card

**Card**:
The rendered Recipe: title block, Credit, the table, Notes, imprint and
Stains, as one SVG. The same Card serves screen, download and print.
_Avoid_: view, page (the page holds a Card)

**Layout**:
Rows, columns and spans computed from the Tree. Never stored.

**Fit**:
Whether the Card prints on one US Letter page with cell text at 8 pt or
larger, in the better orientation. Checked at submission.
_Avoid_: overflow, too big

**Stain**:
A faint decorative mark on a Card (coffee ring, grease halo, flour, tomato,
pencil tick), seeded from the Recipe id, placed only in margins, never
printed. Well-used Cards earn one more.

**Chest**:
The home page: the add form and every Dish with its Labels.
_Avoid_: index, feed, catalogue

### Dishes and variants

**Dish**:
What is being cooked ("Chicken curry"), with a name per language and no
language of its own. Groups Variants.
_Avoid_: category, group, recipe (a Recipe is one Variant)

**Variant**:
One Recipe within its Dish, numbered at filing ("variant 3"); the number is
never reused or changed while it stays in the Dish.
_Avoid_: version, alternative

**Reference**:
The Variant the others are compared against: the first filed, unless the
Owner promotes another.
_Avoid_: base, original, canonical recipe

**Label**:
A few words that tell a Variant apart ("with coconut milk & spinach"),
written by the agent, edited by the Owner.
_Avoid_: subtitle, tag

**Canonical name**:
An Ingredient's matching key: English, lowercase, singular ("chicken"),
whatever the source language. Never shown.
_Avoid_: normalized name, slug

**Chip**:
One computed difference from the Reference: added, changed or missing.
_Avoid_: diff, tag

**Fold**:
Merging a copy of an existing Variant into it: same canonical ingredients
and amounts, within one of slack. The copy's URL joins **Also found at**.
_Avoid_: dedupe (that is the Claim, for the same URL or text)

**Claim**:
The record that makes one source (a normalized URL, a pasted text's hash,
or a card and language) start at most one Run.

### Languages

**Overlay**:
A Translation's words keyed by the ids of the one Tree; it cannot change
structure.
_Avoid_: localized recipe, copy

**Translation**:
A Run that writes one Overlay for one card in one language.
_Avoid_: localisation

**Catalog**:
The committed page text for one language (`internal/i18n/<lang>.json`).
_Avoid_: strings file, locale

### Running

**Run**:
One attempt to turn a source into a Recipe: one agent session, from
Dispatched to Ready or Failed. Resumes continue the same Run; a retry starts
a new one.
_Avoid_: job, task, generation

**Status**:
Dispatched (saved, its /work request on the way) → Running (a lease is held)
→ Ready | Failed.
_Avoid_: Queued (there is no queue)

**Lease**:
`LeaseUntil` on the record: the Run's ceiling plus slack. While it is live,
another /work for the record answers 409; when it lapses, the Tick resumes.

**Submission**:
One call of `submit_recipe` by the agent. Validated; a rejection goes back
to the agent as a tool error with the reasons.
_Avoid_: result, output

**Rejection**:
A Submission that failed validation, by kind: schema, tree or fit.

**Tick**:
`POST /tick` from Cloud Scheduler every 15 minutes: re-dispatches lost and
stalled Runs. The only clock.
_Avoid_: cron, sweep

**Trace**:
The append-only list of what happened to a Recipe's Runs: dispatches,
fetches, Rejections and their reasons, failures, resumes. Shown to the Owner.
_Avoid_: log (that's Cloud Logging)

**Meters**:
Raw counts from session usage, accumulated across Runs: tokens by kind,
fetches, active seconds, the vendor's list cost in cents, Submissions and
Rejections by kind, resumes.
_Avoid_: cost (dollars come from the vendor)

### People

**Visitor**:
Anyone with the link. Reads every Ready Card and may file new Recipes,
within the daily caps.
_Avoid_: user (there are no accounts in stage 1)

**Owner**:
The one person who logs in with the owner passphrase: deletes, retries,
sees the admin page and Traces.
_Avoid_: admin (until stage 2 adds the admin flag)
