# @paxeer-network/sdk-docs

paxeer-network SDK documentation runtime with MCP server support.

## MCP Install (Claude/Codex/Cursor/OpenCode)

Install paxeer-network SDK Docs MCP configuration into supported clients:

```bash
# Direct install from sdk-docs CLI
paxeer-network-sdk-docs-mcp install --client all --scope user
```

You can also install via the devkit forwarding command:

```bash
paxeer-network-devkit mcp install --client all --scope user
```

### Supported clients

- `claude`
- `codex`
- `cursor`
- `opencode`
- `all` (default)

### Common options

- `--client <claude|codex|cursor|opencode|all>`: target client(s). Comma-separated values are supported.
- `--scope <user|project>`: install to user-level config or current project config.
- `--dry-run`: preview target paths/actions without changing files.
- `--force`: overwrite paxeer-network-owned MCP keys for an existing same-name entry.
- `--name <serverName>`: MCP server key in `mcpServers` (default: `paxeer-network-sdk-docs`).

### Examples

```bash
# Preview project-level Cursor install
paxeer-network-sdk-docs-mcp install --client cursor --scope project --dry-run

# Install for Claude + Codex at project scope
paxeer-network-sdk-docs-mcp install --client claude,codex --scope project

# Force refresh a custom server name
paxeer-network-sdk-docs-mcp install --client opencode --name paxeer-network-sdk-docs --force
```
