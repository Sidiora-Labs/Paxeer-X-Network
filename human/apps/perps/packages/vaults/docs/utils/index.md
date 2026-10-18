# utils

## Overview

Utility functions for vaults UI: markdown link parsing, operation payload type mapping, and operation status color.

## Files

| File | Language | Description |
|------|----------|-------------|
| [parseMarkdownLinks](./parseMarkdownLinks.md) | TSX | Parses [text](url) in strings and returns ReactNode[] with &lt;a&gt; elements |
| [operationPayload](./operationPayload.md) | TypeScript | getToAccountPayloadType(OperationType, RoleType) → 0–3 |
| [getOperationStatusColor](./getOperationStatusColor.md) | TypeScript | Maps status string to "success" \| "danger" \| "primary" |
