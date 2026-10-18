import type { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { z } from "zod";
import type { AiDocsFacade } from "../facade.js";

type GuardFacadeResult =
  | { ok: true; facade: AiDocsFacade }
  | { ok: false; payload: string };

type ToolContext = {
  /** Provides a validated facade or a serialized error payload. */
  guardFacade: () => GuardFacadeResult;
};

type ToolDefinition = {
  /** MCP tool identifier exposed to clients. */
  name: string;
  /** Human-readable tool contract for model/tooling selection. */
  description: string;
  /** Optional zod input schema used by MCP SDK validation. */
  inputSchema?: Record<string, z.ZodTypeAny>;
  /** Executes the tool and returns JSON-serializable payload. */
  run(args: Record<string, unknown> | undefined, facade: AiDocsFacade): unknown;
};

/** Normalizes all tool outputs into plain text JSON blocks. */
function jsonContent(payload: unknown) {
  return {
    content: [
      { type: "text" as const, text: JSON.stringify(payload, null, 2) },
    ],
  };
}

/** Declarative MCP tool catalog for paxeer-network-sdk-docs runtime. */
const DOCS_TOOLS: ToolDefinition[] = [
  {
    name: "paxeerNetwork_docs_health",
    description:
      "Returns paxeer-network SDK docs bundle metadata (gitSha, generatedAt, paths). Use to verify repo-local or embedded generated artifacts.",
    run(_args, facade) {
      const b = facade.bundle;
      return {
        ok: true as const,
        gitSha: b.manifest.gitSha,
        generatedAt: b.manifest.generatedAt,
        releaseVersion: b.manifest.releaseVersion,
        repoRoot: b.repoRoot,
        generatedRoot: b.generatedRoot,
      };
    },
  },
  {
    name: "paxeerNetwork_docs_search",
    description:
      "Search paxeer-network SDK documentation (narrative markdown, recipes, workflows). Returns a DocsResult with ranked narrativeHits; when present, matchedEntityId or keywordSingletonId point to a concrete symbol — call paxeerNetwork_docs_get_component, paxeerNetwork_docs_get_type, or paxeerNetwork_docs_get_hook next (check matchedEntityKind for the right tool). Response may also include queryVariants, inferredPackages, and packageSurfaceHints to guide follow-up exact lookups. Optional filters: k (max hits), kinds, packages.",
    inputSchema: {
      query: z.string().min(1).describe("Free-text search query"),
      k: z.number().int().min(1).max(50).optional().describe("Max hits (1-50)"),
      kinds: z.array(z.string()).optional(),
      packages: z.array(z.string()).optional(),
    },
    run(args, facade) {
      return facade.searchDocs({
        query: String(args?.query ?? ""),
        k: args?.k as number | undefined,
        kinds: args?.kinds as string[] | undefined,
        packages: args?.packages as string[] | undefined,
      });
    },
  },
  {
    name: "paxeerNetwork_docs_get_component",
    description:
      "Exact component metadata (props, etc.) by symbol, @package/name:Export, component.* entity id, or interceptor target path (e.g. Trading.OrderEntry.SubmitSection). For interceptor targets without a concrete component entity, returns a type-backed fallback with propsType metadata. Returns DocsResult JSON.",
    inputSchema: {
      query: z
        .string()
        .min(1)
        .describe("Symbol, @package/name:Export, or component.* id"),
    },
    run(args, facade) {
      return facade.getComponent(String(args?.query ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_component_doc",
    description:
      "Component markdown (examples / narrative) by symbol, @package/name:Export, or component.* id. For props/types use paxeerNetwork_docs_get_component first.",
    inputSchema: {
      query: z
        .string()
        .min(1)
        .describe("Symbol, @package/name:Export, or component.* id"),
    },
    run(args, facade) {
      return facade.getComponentDoc(String(args?.query ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_type",
    description:
      "Exact type / enum metadata by symbol, @package/name:Name, or type.* entity id. Returns DocsResult JSON.",
    inputSchema: { query: z.string().min(1) },
    run(args, facade) {
      return facade.getType(String(args?.query ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_hook",
    description:
      "Exact hook metadata (params, returns, jsDoc) by symbol name, @package/name:HookName, or hook.* entity id. Hooks (`useMarkPrice`, `useOrderEntry`, etc.) are the primary API for plugin developers. Hook `returns` types may be unions that are not valid React children until narrowed (e.g. `typeof x === 'number'` before rendering in JSX). Returns DocsResult JSON.",
    inputSchema: {
      query: z
        .string()
        .min(1)
        .describe("Symbol name, @package/name:HookName, or hook.* id"),
    },
    run(args, facade) {
      return facade.getHook(String(args?.query ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_package_surface",
    description:
      "Lists export names for an npm package (e.g. @paxeer-network/ui). Returns DocsResult JSON.",
    inputSchema: { packageName: z.string().min(1) },
    run(args, facade) {
      return facade.getPackageSurface(String(args?.packageName ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_release_context",
    description:
      "Manifest + optional build-stamp.json for gitSha / generatedAt. Returns DocsResult JSON.",
    run(_args, facade) {
      return facade.getReleaseContext();
    },
  },
  {
    name: "paxeerNetwork_docs_get_workflow",
    description:
      "Curated workflow markdown (slug without .md). Returns DocsResult JSON.",
    inputSchema: {
      slug: z
        .string()
        .min(1)
        .describe("e.g. wallet-connect for workflows/wallet-connect.md"),
    },
    run(args, facade) {
      return facade.getWorkflow(String(args?.slug ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_recipe",
    description:
      "Minimal recipe markdown (name without .md). Returns DocsResult JSON.",
    inputSchema: { name: z.string().min(1) },
    run(args, facade) {
      return facade.getRecipe(String(args?.name ?? ""));
    },
  },
  {
    name: "paxeerNetwork_docs_get_guardrails",
    description:
      "Guardrails and safety constraints markdown. Returns DocsResult JSON.",
    run(_args, facade) {
      return facade.getGuardrails();
    },
  },
  {
    name: "paxeerNetwork_docs_fetch_sdk_source",
    description:
      "Fetch source file text from the public paxeer-network JS SDK GitHub repo (PaxeerNetworkNetwork/js-sdk). Default ref is branch `main` unless `PAXEER_NETWORK_SDK_GITHUB_REF` is set. If that ref returns HTTP 404 (e.g. unpublished local SHA), the tool retries once on `main`. Repo-relative paths only (e.g. packages/ui/src/button/button.tsx). Optional line/endLine returns excerpt; data.text is always full file.",
    inputSchema: {
      relPath: z
        .string()
        .min(1)
        .describe("Repo-relative path, e.g. packages/ui/src/avatar/avatar.tsx"),
      line: z
        .number()
        .int()
        .min(1)
        .optional()
        .describe("1-based line for excerpt center or range start"),
      endLine: z
        .number()
        .int()
        .min(1)
        .optional()
        .describe("1-based inclusive end line (range with line)"),
      contextLines: z
        .number()
        .int()
        .min(0)
        .max(200)
        .optional()
        .describe("Half-window around line when only line is set (default 15)"),
    },
    run(args, facade) {
      return facade.fetchSdkSource({
        relPath: String(args?.relPath ?? ""),
        line: args?.line as number | undefined,
        endLine: args?.endLine as number | undefined,
        contextLines: args?.contextLines as number | undefined,
      });
    },
  },
];

/** Registers all docs tools from a single declarative registry. */
export function registerDocsTools(server: McpServer, ctx: ToolContext): void {
  for (const tool of DOCS_TOOLS) {
    server.registerTool(
      tool.name,
      {
        description: tool.description,
        ...(tool.inputSchema ? { inputSchema: tool.inputSchema } : {}),
      },
      async (args) => {
        const guard = ctx.guardFacade();
        if (!guard.ok)
          return { content: [{ type: "text" as const, text: guard.payload }] };
        const payload = await tool.run(
          args as Record<string, unknown> | undefined,
          guard.facade,
        );
        return jsonContent(payload);
      },
    );
  }
}
