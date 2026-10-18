import type { LoadedBundle } from "./bundle.js";
import { errResult, okResult } from "./envelope.js";
import { assertSafeRelPath } from "./paths.js";
import type { DocsResult } from "./types.js";

/** Max raw response body size to load into memory (MCP / accidental huge files). */
const MAX_RAW_BYTES = 2 * 1024 * 1024;

const DEFAULT_OWNER = "PaxeerNetworkNetwork";
const DEFAULT_REPO = "js-sdk";
const DEFAULT_BRANCH = "main";
const DEFAULT_CONTEXT_LINES = 15;

export type FetchSdkSourceInput = {
  /** Repo-relative path (POSIX), same shape as symbol `sourcePath`. */
  relPath: string;
  /** 1-based start line for excerpt (optional). */
  line?: number;
  /** 1-based end line inclusive (optional). With `line`, defines range; ignored window logic if both set. */
  endLine?: number;
  /** Half-window size when only `line` is set (default 15). */
  contextLines?: number;
};

export type SdkGithubSourceExcerpt = {
  startLine: number;
  endLine: number;
  text: string;
};

export type SdkGithubSourceData = {
  relPath: string;
  ref: string;
  rawUrl: string;
  blobUrl: string;
  /** Full file contents from the fetched blob. */
  text: string;
  /** Present when line options were used. */
  excerpt?: SdkGithubSourceExcerpt;
};

function githubOwner(): string {
  return (
    (process.env.PAXEER_NETWORK_SDK_GITHUB_OWNER || DEFAULT_OWNER).trim() ||
    DEFAULT_OWNER
  );
}

function githubRepo(): string {
  return (
    (process.env.PAXEER_NETWORK_SDK_GITHUB_REPO || DEFAULT_REPO).trim() || DEFAULT_REPO
  );
}

/**
 * Prefers `PAXEER_NETWORK_SDK_GITHUB_REF`; otherwise `main` so public raw URLs work without a
 * published monorepo-local commit. (Manifest `gitSha` is not used here intentionally.)
 */
function resolvePreferredRef(): string {
  const fromEnv = process.env.PAXEER_NETWORK_SDK_GITHUB_REF?.trim();
  if (fromEnv) return fromEnv;
  return DEFAULT_BRANCH;
}

function normalizeRelPath(raw: string): string {
  const s = raw.trim().replace(/\\/g, "/");
  return s.replace(/^\/+/, "");
}

/**
 * Builds `raw.githubusercontent.com` URL for a repo-relative file path.
 */
export function buildGithubRawUrl(
  owner: string,
  repo: string,
  ref: string,
  relPath: string,
): string {
  const enc = relPath
    .split("/")
    .map((seg) => encodeURIComponent(seg))
    .join("/");
  return `https://raw.githubusercontent.com/${owner}/${repo}/${ref}/${enc}`;
}

/**
 * Builds human-readable `github.com/.../blob/...` URL; optional fragment for line range.
 */
export function buildGithubBlobUrl(
  owner: string,
  repo: string,
  ref: string,
  relPath: string,
  fragment?: { startLine: number; endLine: number },
): string {
  const enc = relPath
    .split("/")
    .map((seg) => encodeURIComponent(seg))
    .join("/");
  let url = `https://github.com/${owner}/${repo}/blob/${ref}/${enc}`;
  if (fragment) {
    const { startLine, endLine } = fragment;
    url +=
      startLine === endLine ? `#L${startLine}` : `#L${startLine}-L${endLine}`;
  }
  return url;
}

function splitLines(text: string): string[] {
  return text.split(/\r\n|\r|\n/);
}

/**
 * Computes 1-based excerpt range within [1, lineCount].
 */
function computeExcerptRange(
  lineCount: number,
  line: number | undefined,
  endLine: number | undefined,
  contextLines: number,
): { ok: true; start: number; end: number } | { ok: false; message: string } {
  if (lineCount === 0) {
    return { ok: false, message: "File is empty." };
  }
  const ctx = Math.max(0, Math.min(200, contextLines));

  if (line === undefined && endLine === undefined) {
    return { ok: false, message: "Internal: no line options." };
  }

  let start: number;
  let end: number;

  if (line !== undefined && endLine !== undefined) {
    if (!Number.isInteger(line) || !Number.isInteger(endLine)) {
      return { ok: false, message: "`line` and `endLine` must be integers." };
    }
    if (line < 1) {
      return { ok: false, message: "`line` must be >= 1." };
    }
    if (endLine < line) {
      return { ok: false, message: "`endLine` must be >= `line`." };
    }
    start = line;
    end = endLine;
  } else if (line !== undefined) {
    if (!Number.isInteger(line)) {
      return { ok: false, message: "`line` must be an integer." };
    }
    if (line < 1) {
      return { ok: false, message: "`line` must be >= 1." };
    }
    start = Math.max(1, line - ctx);
    end = Math.min(lineCount, line + ctx);
  } else {
    /* endLine only */
    if (!Number.isInteger(endLine!)) {
      return { ok: false, message: "`endLine` must be an integer." };
    }
    if (endLine! < 1) {
      return { ok: false, message: "`endLine` must be >= 1." };
    }
    start = 1;
    end = endLine!;
  }

  start = Math.max(1, Math.min(start, lineCount));
  end = Math.max(start, Math.min(end, lineCount));
  return { ok: true, start, end };
}

/**
 * Fetches a source file from the public paxeer-network JS SDK GitHub repo (default ref: branch `main`).
 */
export async function fetchSdkSource(
  bundle: LoadedBundle,
  input: FetchSdkSourceInput,
  started: number,
): Promise<DocsResult<SdkGithubSourceData>> {
  const relPath = normalizeRelPath(input.relPath);
  if (!relPath) {
    return errResult(
      "INVALID_INPUT",
      "`relPath` must be non-empty.",
      undefined,
      bundle,
    );
  }
  if (relPath.includes("://")) {
    return errResult(
      "INVALID_INPUT",
      "`relPath` must be a relative repo path, not a URL.",
      undefined,
      bundle,
    );
  }
  try {
    assertSafeRelPath(relPath);
  } catch {
    return errResult(
      "INVALID_INPUT",
      "Invalid `relPath` (no `..` segments).",
      undefined,
      bundle,
    );
  }

  const owner = githubOwner();
  const repo = githubRepo();
  let ref = resolvePreferredRef();

  /**
   * If `PAXEER_NETWORK_SDK_GITHUB_REF` points at a SHA not on GitHub, retry once on `main`
   * so local-only commits don't produce permanent 404s.
   */
  const tryRefs = ref === DEFAULT_BRANCH ? [ref] : [ref, DEFAULT_BRANCH];

  const wantsExcerpt = input.line !== undefined || input.endLine !== undefined;
  let fragment: { startLine: number; endLine: number } | undefined;

  if (wantsExcerpt) {
    if (
      input.line !== undefined &&
      (!Number.isInteger(input.line) || input.line < 1)
    ) {
      return errResult(
        "INVALID_INPUT",
        "`line` must be a positive integer.",
        undefined,
        bundle,
      );
    }
    if (
      input.endLine !== undefined &&
      (!Number.isInteger(input.endLine) || input.endLine < 1)
    ) {
      return errResult(
        "INVALID_INPUT",
        "`endLine` must be a positive integer.",
        undefined,
        bundle,
      );
    }
    if (
      input.line !== undefined &&
      input.endLine !== undefined &&
      input.endLine < input.line
    ) {
      return errResult(
        "INVALID_INPUT",
        "`endLine` must be >= `line`.",
        undefined,
        bundle,
      );
    }
  }

  const headers: HeadersInit = {
    Accept: "text/plain,*/*;q=0.1",
    "User-Agent": "paxeer-network-sdk-docs-fetch",
  };
  const token =
    process.env.GITHUB_TOKEN?.trim() ||
    process.env.PAXEER_NETWORK_GITHUB_TOKEN?.trim();
  if (token) {
    (headers as Record<string, string>).Authorization = `Bearer ${token}`;
  }

  let res!: Response;
  let rawUrl = "";
  for (let i = 0; i < tryRefs.length; i++) {
    const attemptRef = tryRefs[i]!;
    ref = attemptRef;
    rawUrl = buildGithubRawUrl(owner, repo, attemptRef, relPath);
    try {
      res = await fetch(rawUrl, { redirect: "follow", headers });
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e);
      return errResult(
        "BACKEND_UNAVAILABLE",
        `Failed to reach GitHub: ${msg}`,
        "Check network, proxy, or try again.",
        bundle,
      );
    }
    if (res.status !== 404 || i === tryRefs.length - 1) break;
  }

  if (res.status === 404) {
    const preferred = resolvePreferredRef();
    return errResult(
      "NOT_FOUND",
      `GitHub raw content not found (HTTP 404) for path "${relPath}".`,
      [
        `URL: ${rawUrl}`,
        `Tried ref(s): ${tryRefs.join(", ")}.`,
        preferred !== DEFAULT_BRANCH
          ? `If "${preferred}" is a local-only commit, set PAXEER_NETWORK_SDK_GITHUB_REF=main or omit it.`
          : "Confirm the file path exists on the public js-sdk repo at this ref.",
      ].join(" "),
      bundle,
    );
  }
  if (!res.ok) {
    return errResult(
      "BACKEND_UNAVAILABLE",
      `GitHub returned HTTP ${res.status} for raw content.`,
      `URL: ${rawUrl}`,
      bundle,
    );
  }

  const lenHeader = res.headers.get("content-length");
  if (lenHeader) {
    const n = parseInt(lenHeader, 10);
    if (Number.isFinite(n) && n > MAX_RAW_BYTES) {
      return errResult(
        "INVALID_INPUT",
        `Refused to fetch file: Content-Length ${n} exceeds limit ${MAX_RAW_BYTES}.`,
        "Use a smaller file or increase limit in source if appropriate.",
        bundle,
      );
    }
  }

  const buf = await res.arrayBuffer();
  if (buf.byteLength > MAX_RAW_BYTES) {
    return errResult(
      "INVALID_INPUT",
      `Response size ${buf.byteLength} exceeds limit ${MAX_RAW_BYTES}.`,
      undefined,
      bundle,
    );
  }

  const text = new TextDecoder("utf-8", { fatal: false }).decode(buf);
  const lines = splitLines(text);
  const lineCount = lines.length;

  if (wantsExcerpt && input.line !== undefined && input.line > lineCount) {
    return errResult(
      "INVALID_INPUT",
      `Requested line ${input.line} is past end of file (${lineCount} lines).`,
      undefined,
      bundle,
    );
  }

  let excerpt: SdkGithubSourceExcerpt | undefined;
  let blobUrl: string;

  if (wantsExcerpt) {
    const range = computeExcerptRange(
      lineCount,
      input.line,
      input.endLine,
      input.contextLines ?? DEFAULT_CONTEXT_LINES,
    );
    if (!range.ok) {
      return errResult("INVALID_INPUT", range.message, undefined, bundle);
    }
    fragment = { startLine: range.start, endLine: range.end };
    const slice = lines.slice(range.start - 1, range.end);
    excerpt = {
      startLine: range.start,
      endLine: range.end,
      text: slice.join("\n"),
    };
    blobUrl = buildGithubBlobUrl(owner, repo, ref, relPath, fragment);
  } else {
    blobUrl = buildGithubBlobUrl(owner, repo, ref, relPath);
  }

  const data: SdkGithubSourceData = {
    relPath,
    ref,
    rawUrl,
    blobUrl,
    text,
    ...(excerpt !== undefined ? { excerpt } : {}),
  };

  return okResult(
    bundle,
    data,
    "exact",
    [
      {
        path: relPath,
        why: "github raw + resolved ref",
        startLine: fragment?.startLine,
        endLine: fragment?.endLine,
      },
    ],
    started,
  );
}
