// File generated from our OpenAPI spec by Scalar. See README.md for details.

import { hasOwn } from './values';
import { type ChainflowDataGateway } from '../../client';
import { RequestOptions } from '../request-options';

type LogFn = (message: string, ...rest: unknown[]) => void;
export type Logger = {
  error: LogFn;
  warn: LogFn;
  info: LogFn;
  debug: LogFn;
};
export type LogLevel = 'off' | 'error' | 'warn' | 'info' | 'debug';

const levelNumbers = {
  off: 0,
  error: 200,
  warn: 300,
  info: 400,
  debug: 500,
};

export const parseLogLevel = (
  maybeLevel: string | undefined,
  sourceName: string,
  client: ChainflowDataGateway,
): LogLevel | undefined => {
  if (!maybeLevel) {
    return undefined;
  }
  if (hasOwn(levelNumbers, maybeLevel)) {
    return maybeLevel;
  }
  loggerFor(client).warn(
    `${sourceName} was set to ${JSON.stringify(maybeLevel)}, expected one of ${JSON.stringify(
      Object.keys(levelNumbers),
    )}`,
  );
  return undefined;
};

function noop() {}

function makeLogFn(fnLevel: keyof Logger, logger: Logger | undefined, logLevel: LogLevel) {
  if (!logger || levelNumbers[fnLevel] > levelNumbers[logLevel]) {
    return noop;
  } else {
    // Don't wrap logger functions, we want the stacktrace intact!
    return logger[fnLevel].bind(logger);
  }
}

const noopLogger = {
  error: noop,
  warn: noop,
  info: noop,
  debug: noop,
};

let cachedLoggers = /* @__PURE__ */ new WeakMap<Logger, [LogLevel, Logger]>();

export function loggerFor(client: ChainflowDataGateway): Logger {
  const logger = client.logger;
  const logLevel = client.logLevel ?? 'off';
  if (!logger) {
    return noopLogger;
  }

  const cachedLogger = cachedLoggers.get(logger);
  if (cachedLogger && cachedLogger[0] === logLevel) {
    return cachedLogger[1];
  }

  const levelLogger = {
    error: makeLogFn('error', logger, logLevel),
    warn: makeLogFn('warn', logger, logLevel),
    info: makeLogFn('info', logger, logLevel),
    debug: makeLogFn('debug', logger, logLevel),
  };

  cachedLoggers.set(logger, [logLevel, levelLogger]);

  return levelLogger;
}

/** Value a credential is replaced with before it reaches a log line. */
const REDACTED = '***';

/**
 * Header names whose value is replaced before a request or response is logged.
 *
 * The fixed entries are the conventional credential headers, which is all a runtime shared by every
 * SDK could know on its own. The generated ones are the headers *this* SDK's auth schemes actually
 * send: an OpenAPI `apiKey` scheme names its own header, and `X-Auth-Token` or `PRIVATE-TOKEN` is no
 * less a credential for being spelled differently. Without them a `logLevel: "debug"` run prints the
 * credential in clear text, and the generated CLI turns that level on with `--debug`, so it reaches
 * terminals and CI logs alike.
 */
const REDACTED_HEADERS: ReadonlySet<string> = new Set([
  'authorization',
  'api-key',
  'x-api-key',
  'cookie',
  'set-cookie',
]);

/**
 * Query parameters this SDK sends a credential in, from its `apiKey` schemes with `in: query`.
 *
 * A credential in the query string is worse off than one in a header: the request URL is logged at
 * `info` as well as at `debug`, so it leaks a level below the one a reader would think of as
 * verbose.
 */
const REDACTED_QUERY_PARAMS: readonly string[] = ['token'];

/**
 * Blanks the userinfo of a URL, which is a credential wherever it appears.
 *
 * A `--base-url` carries whatever the caller typed, and this repo already treats userinfo as secret
 * where it handles a base URL elsewhere (`profileKey` blanks it before a URL becomes a keychain
 * account). Both halves go rather than the password alone: a token is as often the username
 * (`https://<token>@host`) as the password, so keeping either back still prints one shape in full.
 */
const redactUserinfo = (url: string): string => {
  const scheme = url.indexOf('://');
  if (scheme === -1) return url;
  const start = scheme + 3;
  // The authority ends at the first of these; an `@` past one of them belongs to a path or a query.
  let end = url.length;
  for (const delimiter of ['/', '?', '#']) {
    const at = url.indexOf(delimiter, start);
    if (at !== -1 && at < end) end = at;
  }
  const at = url.lastIndexOf('@', end);
  return at === -1 || at < start ? url : url.slice(0, start) + REDACTED + url.slice(at);
};

/**
 * Decodes one `application/x-www-form-urlencoded` parameter name, for comparison against the list.
 *
 * A name reaches the URL percent-encoded, so `api%5Fkey` has to match `api_key`. A malformed escape
 * comes back as written rather than throwing: a log line is no place to fail over one, and a name
 * that cannot be decoded cannot match the list anyway.
 */
const decodeQueryName = (name: string): string => {
  try {
    return decodeURIComponent(name.replace(/\+/gu, ' '));
  } catch {
    return name;
  }
};

/**
 * Replaces the value of every credential-bearing query parameter in a URL about to be logged.
 *
 * Rewritten on the query substring rather than through `new URL`, so a relative URL is handled the
 * same way an absolute one is and no parse can throw on the logging path.
 */
export const redactUrl = (url: string): string => {
  const url_ = redactUserinfo(url);
  if (REDACTED_QUERY_PARAMS.length === 0) return url_;
  const mark = url_.indexOf('?');
  if (mark === -1) return url_;
  // A fragment is not part of the query, so splitting it off keeps it out of the rewrite.
  const fragment = url_.indexOf('#', mark);
  const end = fragment === -1 ? url_.length : fragment;
  // Each pair is rewritten where it stands rather than round-tripped through `URLSearchParams`,
  // whose serializer re-spells every *other* parameter in its own safe set -- a space comes back as
  // `+`, a `~` as `%7E` -- and a logged URL that is not the one the request used is a URL nobody can
  // paste back. Splitting and rejoining on `&` is lossless, so a URL carrying no credential comes
  // out of this exactly as it went in.
  const query = url_
    .slice(mark + 1, end)
    .split('&')
    .map((pair) => {
      const equals = pair.indexOf('=');
      // A parameter with no `=` carries no value, so there is nothing in it to redact.
      if (equals === -1) return pair;
      const name = pair.slice(0, equals);
      return REDACTED_QUERY_PARAMS.includes(decodeQueryName(name)) ? name + '=' + REDACTED : pair;
    })
    .join('&');
  return url_.slice(0, mark + 1) + query + url_.slice(end);
};

/**
 * Replaces credential values in a caller-supplied query object.
 *
 * The client merges its own auth query into the URL, but a caller may pass the same parameter here
 * and `buildURL` lets that value win — so it is as live a credential as the one in the URL.
 */
const redactQuery = (query: object): object => {
  const redacted: Record<string, unknown> = { ...(query as Record<string, unknown>) };
  for (const name of REDACTED_QUERY_PARAMS) {
    if (hasOwn(redacted, name)) redacted[name] = REDACTED;
  }
  return redacted;
};

/** Replaces the value of every credential-bearing header, for a `Headers` or a plain record alike. */
export const redactHeaders = (headers: Headers | Record<string, string>): Record<string, string> =>
  Object.fromEntries(
    (headers instanceof Headers ? [...headers] : Object.entries(headers)).map(([name, value]) => [
      name,
      REDACTED_HEADERS.has(name.toLowerCase()) ? REDACTED : value,
    ]),
  );

/** URL-shaped runs inside free text. Whitespace and quotes end one; see {@link TRAILING_PUNCTUATION}. */
const URL_IN_TEXT = /\bhttps?:\/\/[^\s"'<>]+/gu;

/**
 * Punctuation that ends a sentence or closes a bracket rather than belonging to the URL.
 *
 * Trimmed back after the match instead of being excluded from it, because an IPv6 host carries its
 * own `]` (`http://[::1]:8080/p?api_key=…`). Excluding the character cut the match short at the
 * host and left the query — credential included — in the text.
 */
const TRAILING_PUNCTUATION = /[.,;:!?)\]}]+$/u;

/**
 * Redacts every URL quoted inside free text, leaving the rest of the text alone.
 *
 * Runs for every SDK, not only those with a query credential: Node's `fetch` rejects a URL carrying
 * userinfo with a message that quotes the whole URL back (`Request cannot be constructed from a URL
 * that includes credentials: …`), and that message is logged on the connection-failure path. Gating
 * this on the query list left the password in the log for every SDK whose document happens not to
 * put a credential in the query.
 *
 * Only the matched URL is rewritten, never the whole string: a message is not a query string, and
 * handing one to `URLSearchParams` wholesale would swallow everything after the first parameter and
 * take the diagnostic with it.
 */
const redactUrlsInText = (text: string): string =>
  text.replace(URL_IN_TEXT, (match) => {
    const trailing = match.match(TRAILING_PUNCTUATION)?.[0] ?? '';
    return redactUrl(match.slice(0, match.length - trailing.length)) + trailing;
  });

export const formatRequestDetails = (details: {
  options?: RequestOptions | undefined;
  headers?: Headers | Record<string, string> | undefined;
  retryOfRequestLogID?: string | undefined;
  retryOf?: string | undefined;
  url?: string | undefined;
  status?: number | undefined;
  method?: string | undefined;
  durationMs?: number | undefined;
  message?: unknown;
  body?: unknown;
}) => {
  if (details.options) {
    // Swept through a record view rather than field by field: not every field below is declared on
    // `RequestOptions` in every profile (`serverURL` belongs to the Speakeasy compatibility one),
    // and the copy is what keeps the caller's own options object untouched.
    const options: Record<string, unknown> = { ...details.options };
    delete options['headers']; // redundant + leaks internals
    // The Speakeasy profile re-adds a header bag under `fetchOptions`, and the client reads it as
    // the request's headers whenever the flattened one is absent -- so it holds the credential a
    // migrated call site passes. Dropped for the same reason as the field above rather than
    // redacted, because `HeadersLike` also takes shapes (an array of pairs, `null`) that the header
    // redaction does not, and the sent headers are already reported beside this, redacted.
    const fetchOptions = options['fetchOptions'];
    if (fetchOptions && typeof fetchOptions === 'object') {
      const copy: Record<string, unknown> = { ...(fetchOptions as Record<string, unknown>) };
      delete copy['headers'];
      options['fetchOptions'] = copy;
    }
    const query = options['query'];
    if (query && typeof query === 'object') options['query'] = redactQuery(query);
    // These three are URL strings, not route fragments: `buildURL` takes an absolute `path` whole --
    // userinfo, query string and all -- while `defaultBaseURL` and the Speakeasy profile's
    // `serverURL` each replace the base URL outright. The caller-supplied escape hatches therefore
    // carry a credential exactly as readily as the resolved URL beside them does. `serverURL` may
    // be a `URL`, whose `username` and `password` print as fields of their own, so the redacted
    // string goes back rather than the object.
    for (const field of ['path', 'defaultBaseURL', 'serverURL']) {
      const value = options[field];
      if (typeof value === 'string') options[field] = redactUrl(value);
      else if (value instanceof URL) options[field] = redactUrl(value.toString());
    }
    details.options = options as RequestOptions;
  }
  if (details.url) {
    details.url = redactUrl(details.url);
  }
  if (details.headers) {
    details.headers = redactHeaders(details.headers);
  }
  // The message is whatever the fetch implementation threw, and Deno's connection errors quote the
  // request URL inside their text (see the note at the `connection failed` call sites), so a query
  // credential rides along in a field none of the checks above look at.
  if (typeof details.message === 'string') {
    details.message = redactUrlsInText(details.message);
  }
  if ('retryOfRequestLogID' in details) {
    if (details.retryOfRequestLogID) {
      details.retryOf = details.retryOfRequestLogID;
    }
    delete details.retryOfRequestLogID;
  }
  return details;
};
