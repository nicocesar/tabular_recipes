# tabular_recipes

The code behind **recipes.nicocesar.com**: give it a recipe URL (or paste the
text) and a Claude Managed Agent rewrites it as a tree in the *Cooking for
Engineers* tabular style. The server lays the tree out as a card — an SVG
table with ingredients down the left and each step a box spanning what it
combines — for the kitchen screen and for one printed Letter page.

Go standard library, server-rendered HTML, Datastore, scale-to-zero Cloud Run.

## Run it locally

```sh
make demo     # free stand-in agent; files a demo card (owner passphrase: "owner")
make run      # set ANTHROPIC_API_KEY first to use the real agent (costs money)
make test
```

Open http://localhost:8080. Data lives in `./data`.

## Where things are

| | |
|---|---|
| `internal/recipe` | the Tree, its validator, the layout (ADR 0002) |
| `internal/card` | the SVG card: font metrics, wrapping, glyph outlines, the one-page fit check, stains (ADRs 0003, 0004, 0011) |
| `internal/tabulate` | the tabulator agent, a Run, dispatch and the Tick (ADRs 0005–0007, 0013) |
| `internal/store` | Datastore and the filesystem store (ADRs 0001, 0010) |
| `internal/httpapi` | pages, the add form and its caps, the owner's workbench (ADRs 0008, 0009, 0012) |
| `cmd/server` | `main`, templates, static files |
| `docs/adr` | every decision; `CONTEXT.md` has the vocabulary |
| `SETUP.md` | one-time GCP and Anthropic setup |

## Licences

Fonts in `internal/card/fonts`: Special Elite (Apache 2.0), Courier Prime and
Source Serif 4 (SIL OFL 1.1). Licence files are alongside.
