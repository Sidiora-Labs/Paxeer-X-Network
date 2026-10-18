# CLI

The `i18n` binary ships with `@paxeer-network/i18n`. It helps maintain locale files: CSV/JSON conversion, CSV diffs, merging default and `extend/` JSON, splitting by key prefix, and filtering keys. The binary name is **`i18n`** (see `package.json` `bin`).

**Equivalent invocations** (use whichever fits your setup):

- `pnpm exec i18n` — when the package is a dependency in a pnpm workspace
- `npx i18n` — when installed locally (`node_modules/.bin`)
- `npx @paxeer-network/i18n` — runs the published package without a prior install

Below, examples use `npx @paxeer-network/i18n`; substitute `pnpm exec i18n` or `npx i18n` as needed.

**Help:** run `npx @paxeer-network/i18n --help` for the command list, or `npx @paxeer-network/i18n <command> --help` for a subcommand (e.g. positionals and flags for `csv2json`).

**Audience:** Paths like `./locales` are examples. When maintaining **this package**, commands are often run from `packages/i18n`; when using the CLI **inside another repo**, point arguments at your project’s directories. `filterKeys` defaults to this package’s `locales` folder relative to the installed package unless you pass `--locales-dir`.

## Command reference

| Command        | Purpose                                            | Arguments                                                            |
| -------------- | -------------------------------------------------- | -------------------------------------------------------------------- |
| `csv2json`     | CSV → multiple per-locale JSON files               | `<input.csv> <outputDir>`                                            |
| `json2csv`     | Directory of JSON → one CSV                        | `<inputDir> <output.csv>`                                            |
| `diffcsv`      | Compare two CSV files                              | `<oldFile> <newFile>`                                                |
| `generateCsv`  | JSON directory → CSV (e.g. release / template)     | `<inputDir> <output.csv>`                                            |
| `fillJson`     | Fill a locale JSON from the English key set        | `<input.json> <output.json>`                                         |
| `separateJson` | Split keys by prefix into root vs `extend/`        | `<inputDir> <outputDir> <keyPrefix>` (prefix may be comma-separated) |
| `mergeJson`    | Merge root + `extend/` JSON per locale             | `<inputDir> <outputDir>`                                             |
| `filterKeys`   | Keep or remove keys by prefix in locale JSON files | See [filterKeys](#filterkeys)                                        |

## Typical workflows

- **Export for translators:** `generateCsv ./locales ./dist/locale.csv` → translate the CSV → `csv2json ./dist/locale.csv ./dist/locales` to get updated JSON.
- **Extend vs main bundles:** Use `separateJson` / `mergeJson` when you keep SDK strings and `extend.*` keys in separate files; align with the `extend/` layout described in [Integration guide](./integration.md).
- **Fill missing keys in a locale:** `fillJson` against a partial locale file to align keys with the built-in English set.

---

### csv2json

Convert one locale CSV into multiple locale JSON files under `<outputDir>`.

```bash
npx @paxeer-network/i18n csv2json <input> <outputDir>
```

**Example:** `npx @paxeer-network/i18n csv2json ./dist/locale.csv ./dist/locales`

### json2csv

Collect all `*.json` in `<inputDir>` and write a single CSV to `<output>`.

```bash
npx @paxeer-network/i18n json2csv <inputDir> <output>
```

**Example:** `npx @paxeer-network/i18n json2csv ./locales ./dist/locale.csv`

### diffcsv

Compare two locale CSV files (useful for reviewing translation updates).

```bash
npx @paxeer-network/i18n diffcsv <oldFile> <newFile>
```

**Example:** `npx @paxeer-network/i18n diffcsv ./dist/locale1.csv ./dist/locale2.csv`

### generateCsv

Build a locale CSV from JSON files in `<inputDir>`. Often used for this package’s own `locales` during build or release.

```bash
npx @paxeer-network/i18n generateCsv <inputDir> <output>
```

**Example:** `npx @paxeer-network/i18n generateCsv ./locales ./dist/locale.csv`

### fillJson

Produce a new JSON file with the **same keys as the built-in English set**, taking values from `<input>` (missing keys become empty). Use to backfill a locale from a template or another language file.

```bash
npx @paxeer-network/i18n fillJson <input> <output>
```

**Example:** `npx @paxeer-network/i18n fillJson ./src/locale/zh.json ./dist/locale/zh.json`

### separateJson

For each locale JSON in `<inputDir>`, split keys by prefix: keys **starting with** the given prefix string(s) go under `<outputDir>/extend/`, the rest stay at `<outputDir>`. The third argument is one or more **prefix strings** separated by commas (e.g. `extend` matches `extend.foo`; `extend,trading` would match both prefixes).

```bash
npx @paxeer-network/i18n separateJson <inputDir> <outputDir> <keyPrefix>
```

**Example:** `npx @paxeer-network/i18n separateJson ./locales ./dist/locales extend`

### mergeJson

Merge “default” JSON files and matching files under `extend/` inside `<inputDir>` into one file per locale in `<outputDir>`. The input layout must match what `separateJson` (or your pipeline) produces.

```bash
npx @paxeer-network/i18n mergeJson <inputDir> <outputDir>
```

**Example:** `npx @paxeer-network/i18n mergeJson ./locales ./dist/locales`

### filterKeys

Keep or remove keys whose names start with `--prefix`. By default, operates on the package’s `locales` directory next to the CLI in the installed package (equivalent to `packages/i18n/locales` in this monorepo). Override with `--locales-dir` for your app’s JSON directory.

You must pass **exactly one** of `--keep` / `-k` or `--remove` / `-r`.

```bash
npx @paxeer-network/i18n filterKeys (--keep | -k | --remove | -r) --prefix <prefix> [--locales-dir <dir>]
```

| Option           | Description                                                      |
| ---------------- | ---------------------------------------------------------------- |
| `--keep`, `-k`   | Keep only keys that start with the prefix.                       |
| `--remove`, `-r` | Remove keys that start with the prefix.                          |
| `--prefix`       | (Required) Prefix to match (e.g. `trading.` or `trading`).       |
| `--locales-dir`  | Directory of locale `*.json` files (default: package `locales`). |

**Examples:**

```bash
npx @paxeer-network/i18n filterKeys --keep --prefix trading.
npx @paxeer-network/i18n filterKeys -r --prefix trading.
npx @paxeer-network/i18n filterKeys -k --prefix extend. --locales-dir ./my-locales
```

See also: [Integration guide](./integration.md) · [Examples](./examples.md)
