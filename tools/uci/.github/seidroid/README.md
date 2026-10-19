# seidroid

`seidroid[bot]` is Sei's AI reviewer and assistant for GitHub. Each subfolder here is a
**seidroid feature** — its base prompts, its driver, and/or the reusable workflow it
publishes. The reusable *workflows* that run these features live in `.github/workflows/`
(`ai-review.yml`, `ai-assistant.yml`); a caller repo wires them with `uses:` and pins
`uci-ref` to the same ref. See each feature's own README for how to enable it.

| Feature | What it is | Trigger |
|---|---|---|
| [`ai-review/`](ai-review/) | The workflow-driven seidroid[bot] helpers and their base prompts: an automatic three-pass PR review (OpenAI Codex ∥ Cursor → Claude synthesis, posting one PR review + an `AI Review` check) and the conversational `@seidroid` assistant. Prompts: `scout.md`, `review.md`, `assistant.md`. | `pull_request` (review); `@seidroid` mention (assistant) |

`ai-review` passes the diff to models from inside the Actions runner, so it sees the change
but never runs it. A sandbox-backed path that could build and test a PR before reviewing it
lived here as `xreview/` and has been removed while its shape is reconsidered; see the pull
request that removed it for what was learned.
