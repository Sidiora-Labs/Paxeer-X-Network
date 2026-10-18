# @paxeer-network/devkit

CLI toolkit for scaffolding plugins and modules, authenticating with Marketplace, submitting and managing plugins, and installing MCP / agent skill integrations.

## Requirements

- **Node.js** v20.19.0 or newer.

## Installation

### From npm

```bash
pnpm add -g @paxeer-network/devkit
# or
npm install -g @paxeer-network/devkit
```

Then run:

```bash
paxeer-network-devkit --help
```

### One-off without global install

```bash
pnpm dlx @paxeer-network/devkit --help
# or
npx @paxeer-network/devkit --help
```

## Authentication

Login opens the Marketplace web login page, where you connect a wallet and sign a message. The signature is exchanged for a CLI token pair, which the web page delivers to a short-lived local callback server.

- **Credentials file:** `~/.paxeer-network/auth.json` (created automatically).
- Access tokens are refreshed automatically; `logout` also revokes the session server-side.
- The callback port must be an integer from **9876 to 9899** — the web page refuses to deliver credentials to any other local port.

```bash
paxeer-network-devkit login              # open browser, connect wallet and sign
paxeer-network-devkit login --force      # re-authenticate even if already logged in
paxeer-network-devkit login --port 9877  # if default port is busy (default is 9876)
paxeer-network-devkit whoami             # show the account and check the session
paxeer-network-devkit logout
```

`whoami` probes an authenticated endpoint, so an expired session is reported as such instead of appearing logged in. When the marketplace is unreachable it warns and still reports the stored account.

**Submitting also requires a linked GitHub account.** Connect it once in the web console (`https://dex.paxeer.network/en/marketplace-listings`); otherwise `submit` fails with a 409.

### Environment variables

| Variable                      | Default                           | Purpose                                                                                                           |
| ----------------------------- | --------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `PAXEER_NETWORK_API_URL`             | `https://dex-api.paxeer.network` | API origin. A legacy `/api`, `/api/marketplace`, or `/api/auth` suffix is stripped.                               |
| `PAXEER_NETWORK_WEB_URL`             | `https://dex.paxeer.network`     | Web console origin used for the login and management links.                                                       |
| `PAXEER_NETWORK_HTTP_TIMEOUT_MS`     | `15000`                           | Per-request timeout for CLI HTTP calls.                                                                           |
| `GCS_PUBLIC_BASE_URL`         | —                                 | Extra origin accepted for marketplace-hosted cover URLs, for running against a local GCS emulator (see `submit`). |
| `PAXEER_NETWORK_DEVKIT_NO_ENV_HINTS` | —                                 | Set to any non-empty value to suppress the post-`create`/`submit` tip about the SDK Docs MCP.                     |
| `PAXEER_NETWORK_MCP_INSTALL_DEBUG`   | —                                 | Set to `1` or `true` to trace `mcp install` child-process failures on stderr.                                     |

## Commands overview

Run `paxeer-network-devkit <command> --help` for options on any command.

### `create`

Scaffold new artifacts (interactive).

| Subcommand      | Description                                                                                                                                                                      |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `create plugin` | Clone and render the plugin template (`PaxeerNetworkNetwork/paxeer-network-plugin-template`), prompt for name/plugin id/interceptor target, and optionally generate `.paxeer-network-manifest.json`. |
| `create module` | Guided flow for module type (`page`, `component`, `hook`, `utils`, `module`). **File generation is not implemented yet**—it only collects choices and prints a summary.          |

```bash
paxeer-network-devkit create plugin
paxeer-network-devkit create module
paxeer-network-devkit create module --name my-module
```

### Marketplace: `submit`, `search`, `list`, `update`, `view`, `disable`

These commands use the Marketplace API. Public read commands (`search`, `view`) do not require login. Owner-scoped commands (`submit`, `list`, `update`, `disable`) require login (`paxeer-network-devkit login`).

**`submit`** — register a new plugin from a local directory.

- Resolves metadata from `package.json` and/or `.paxeer-network-manifest.json`.
- Required: `npmName`, `repoUrl`, `name`, `description`, at least one tag, and at least one cover image. `name` and `description` are prompted for when missing.
- `repoUrl` should be a GitHub URL (`https://github.com/<owner>/<repo>`); it can be filled from `git remote` when missing.
- **Tags** must be from the allowed set (max 5): `UI`, `Indicator`, `Order Entry`, `Trading`, `Chart`, `Portfolio`, `Analytics`, `Market`, `OrderBook`, `Deposit`, `Withdraw`, `Transfer`, `Swap`, `Liquidity`, `Staking`, `Rewards`, `Tool`, `Widget`, `AI`.
- **`coverImages`** (1–5) may be local paths relative to the plugin directory; they are uploaded to the Marketplace and replaced with the managed URLs. `--dry-run` validates them without uploading.
  Existing remote covers must use `https://oss.paxeer.network/marketplace/`. Third-party or malformed URLs are rejected before any upload. For a local GCS emulator, set `GCS_PUBLIC_BASE_URL` to the same origin configured on the backend; `/marketplace/` is still required.
- `pluginId` is optional — the backend derives it from the npm package name when omitted.
- Submissions are queued and audited asynchronously; the command reports the submission status rather than a final listing. Resubmitting before the audit finishes returns a 409 — track the submission with `list` and resubmit once it completes.

```bash
paxeer-network-devkit submit
paxeer-network-devkit submit --path ./my-plugin
paxeer-network-devkit submit -p ./my-plugin --tags UI,Trading --dry-run
```

**`search`** — search public available plugins in Marketplace. `--tag` accepts a tag display name (e.g. `Order Entry`) or its slug (e.g. `order-entry`); display names are converted to slugs automatically.

```bash
paxeer-network-devkit search
paxeer-network-devkit search orderbook
paxeer-network-devkit search orderbook --tag trading --limit 5
paxeer-network-devkit search funding --sort updated --order desc --json
```

**`list`** — list plugins associated with your account, including the latest audit status for each.

```bash
paxeer-network-devkit list
paxeer-network-devkit list --json
```

Note that listings only appear here once the audit worker has created them; a queued submission is not yet a listing.

**`update`** — PATCH plugin metadata for an existing listing (requires `pluginId` in `.paxeer-network-manifest.json`).

Updatable fields: `name`, `description`, `tags`, `coverImages`, `usagePrompt`. Local `coverImages` are uploaded before the PATCH, and only on a real run — `--dry-run` validates them without uploading. An explicit `usagePrompt: null` (or `""`) in the manifest clears the server-side value; a generated manifest simply omits the field.

```bash
paxeer-network-devkit update --path ./my-plugin --dry-run
paxeer-network-devkit update -p ./my-plugin
```

**`view`** — fetch one plugin by ID as JSON.

```bash
paxeer-network-devkit view <plugin-id>
```

**`disable`** — delist one of your plugins (sets its status to `deprecated`). Only `available` plugins can be delisted, and the change cannot be undone from the CLI.

```bash
paxeer-network-devkit disable
paxeer-network-devkit disable --pluginId my-plugin-id
```

> `delete` is no longer supported: the Marketplace API exposes no delete endpoint. Use `disable` to delist, or manage listings in the web console.

### `mcp install`

Install the **paxeer-network SDK Docs** MCP server entry for Claude, Codex, Cursor, OpenCode, etc.

```bash
paxeer-network-devkit mcp install
paxeer-network-devkit mcp install --client cursor --scope project
paxeer-network-devkit mcp install --client all --scope user
paxeer-network-devkit mcp install --sdk-docs-version 0.1.0
paxeer-network-devkit mcp install --dry-run
paxeer-network-devkit mcp install --force
```

| Option               | Description                                                                                                      |
| -------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `--client`           | `claude`, `codex`, `cursor`, `opencode`, `all`, or comma-separated list (default: `all`).                        |
| `--scope`            | `user` or `project` (default: `user`).                                                                           |
| `--name`             | MCP server id in config (default: `paxeer-network-sdk-docs`).                                                           |
| `--sdk-docs-version` | Pin `@paxeer-network/sdk-docs` to a version or dist-tag (e.g. `0.1.0`, `beta`) for this run and written config. |
| `--dry-run`          | Print the planned config changes without writing any files.                                                      |
| `--force`            | Replace an existing MCP entry wholesale instead of merging into it (drops fields you added to the entry).        |

### `mcp detect`

Check whether the **paxeer-network SDK Docs** MCP server is already configured for your coding agents. Claude, Codex, Cursor, and OpenCode are each inspected at both **user** scope (e.g. `~/.cursor/mcp.json`) and **project** scope (e.g. `.cursor/mcp.json` under the current directory). The report lists every config path with its state — `configured` (with the matched server key), `not detected`, `no file`, or `invalid JSON` — and is read-only: config files are never modified.

```bash
paxeer-network-devkit mcp detect
paxeer-network-devkit mcp detect --client cursor
paxeer-network-devkit mcp detect --json
```

| Option     | Description                                                                                       |
| ---------- | ------------------------------------------------------------------------------------------------- |
| `--client` | `claude`, `codex`, `cursor`, `opencode`, `all`, or comma-separated list (default: `all`).         |
| `--json`   | Print a machine-readable JSON report (paths, states, matched server key) instead of human output. |

### `skills install`

Install paxeer-network **agent skills** for plugin workflows (create, write, add, submit) with `npx -y skills add …`.

Default behavior installs four skills non-interactively: `paxeer-network-plugin-create`, `paxeer-network-plugin-write`, `paxeer-network-plugin-add`, `paxeer-network-plugin-submit`.

```bash
paxeer-network-devkit skills install
paxeer-network-devkit skills install --list
paxeer-network-devkit skills install --dry-run
paxeer-network-devkit skills install other/repo --skill my-skill -y
paxeer-network-devkit skills install -- --some-upstream-skills-flag
```

| Option            | Description                                                             |
| ----------------- | ----------------------------------------------------------------------- |
| `[source]`        | GitHub `owner/repo`, URL, or local path (default source is built in).   |
| `--list`          | List skills in the source without installing.                           |
| `--skill` / `-s`  | Repeatable; install only named skills (replaces default four when set). |
| `--all`           | Forward `--all` to the skills CLI.                                      |
| `--global` / `-g` | Global install for the skills CLI.                                      |
| `--agent` / `-a`  | Target agent(s), e.g. `-a cursor`.                                      |
| `--copy`          | Copy files instead of symlinks.                                         |
| `--yes` / `-y`    | With `--list` only: pass `-y` through.                                  |
| `--`              | Everything after `--` is forwarded to the upstream `skills` CLI.        |

## Troubleshooting

- **Marketplace API errors** — check your network connection; if the CLI reports a failed request, try again later or verify you are logged in (`paxeer-network-devkit whoami`).
- **Login: port in use** — run `paxeer-network-devkit login --port <free-port>`.
- **Ctrl+C during prompts** — the CLI handles enquirer cancellation and exits with a clear message when possible.
- **Invalid working directory** — if the shell’s cwd was deleted, the CLI may switch to `HOME` or the package directory and warn you.
- **`mcp install` hangs or fails silently** — set `PAXEER_NETWORK_MCP_INSTALL_DEBUG=1` to trace the child process, and verify `npx` is available on `PATH`.

## License

See the repository root license for this package’s distribution terms.
