# Paxeer X Network documentation site

MkDocs Material site for Paxeer X Network, configured by `mkdocs.yml` in this
directory. Page sources are under `docs/`. Normative protocol text remains in
`spec/`. The hosted documentation is [docs.paxeer.app](https://docs.paxeer.app/).

## Preview locally

From this directory:

```sh
python3 -m venv .venv
. .venv/bin/activate
pip install -r requirements.txt
mkdocs serve
```

Then open the local URL the command prints.

A one-shot build:

```sh
mkdocs build --strict
```

Output lands in `site/`, which is not committed.

`mkdocs serve` and `mkdocs build` must be run from this directory so they
read `mkdocs.yml` here, or be given it with `-f docs/site/mkdocs.yml` from the
repository root.

## Layout

| Path | Role |
| --- | --- |
| `mkdocs.yml` | Site configuration and navigation |
| `requirements.txt` | MkDocs and the Material theme, pinned |
| `docs/` | Markdown pages |
| `build_wiki.py` | Standalone wiki HTML export, below |

The wiki pages remain in `../wiki/`. Prefer this tree for new documentation.

## CI

Pull requests that touch `docs/` run `mkdocs build --strict` through
`.github/workflows/docs-site.yml`. Pushes to `main` that touch `docs/`
deploy the built site to GitHub Pages.

## Wiki HTML export

`build_wiki.py` renders `../wiki/` into static HTML under `out/` (not
committed) and refuses broken relative Markdown links. It is a standalone
Python 3 script and is not part of the MkDocs build:

```sh
python3 docs/site/build_wiki.py
```
