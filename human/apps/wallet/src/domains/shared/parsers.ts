import type {
  Address,
  BaseUnitAmount,
  BoundaryParser,
  ChainId,
  CorrelationId,
  HexData,
  ParseIssue,
  ParseResult,
  UnixMilliseconds,
} from './types';

const ADDRESS_PATTERN = /^0x[0-9a-fA-F]{40}$/;
const HEX_PATTERN = /^0x(?:[0-9a-fA-F]{2})*$/;
const CORRELATION_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._:-]{7,127}$/;

export function issue(
  path: string,
  code: ParseIssue['code'],
  message: string,
): ParseResult<never> {
  return { ok: false, issues: [{ path, code, message }] };
}

export function parseRecord(
  input: unknown,
  path = '$',
): ParseResult<Readonly<Record<string, unknown>>> {
  if (typeof input !== 'object' || input === null || Array.isArray(input)) {
    return issue(path, 'invalid_type', 'Expected an object');
  }
  return { ok: true, value: input as Readonly<Record<string, unknown>> };
}

export function rejectUnknownFields(
  record: Readonly<Record<string, unknown>>,
  allowed: ReadonlySet<string>,
  path = '$',
): ParseResult<void> {
  const unknown = Object.keys(record).filter((key) => !allowed.has(key));
  if (unknown.length > 0) {
    return issue(
      `${path}.${unknown[0]}`,
      'unknown_field',
      `Unknown field: ${unknown[0]}`,
    );
  }
  return { ok: true, value: undefined };
}

export function parseBoundedString(
  input: unknown,
  options: {
    readonly path?: string;
    readonly minLength?: number;
    readonly maxLength: number;
    readonly pattern?: RegExp;
  },
): ParseResult<string> {
  const path = options.path ?? '$';
  if (typeof input !== 'string') {
    return issue(path, 'invalid_type', 'Expected a string');
  }
  const minLength = options.minLength ?? 0;
  if (input.length < minLength || input.length > options.maxLength) {
    return issue(path, 'out_of_bounds', 'String length is outside allowed bounds');
  }
  if (options.pattern && !options.pattern.test(input)) {
    return issue(path, 'invalid_format', 'String format is invalid');
  }
  return { ok: true, value: input };
}

export const parseAddress: BoundaryParser<Address> = (input) => {
  const parsed = parseBoundedString(input, {
    path: '$',
    minLength: 42,
    maxLength: 42,
    pattern: ADDRESS_PATTERN,
  });
  return parsed.ok ? { ok: true, value: parsed.value as Address } : parsed;
};

export const parseChainId: BoundaryParser<ChainId> = (input) => {
  if (
    typeof input !== 'number' ||
    !Number.isSafeInteger(input) ||
    input <= 0
  ) {
    return issue('$', 'invalid_format', 'Chain ID must be a positive safe integer');
  }
  return { ok: true, value: input as ChainId };
};

export const parseBaseUnitAmount: BoundaryParser<BaseUnitAmount> = (input) => {
  if (typeof input !== 'string' || !/^(0|[1-9][0-9]*)$/.test(input)) {
    return issue('$', 'invalid_format', 'Amount must be an unsigned base-10 integer');
  }
  return { ok: true, value: BigInt(input) as BaseUnitAmount };
};

export const parseHexData: BoundaryParser<HexData> = (input) => {
  const parsed = parseBoundedString(input, {
    path: '$',
    minLength: 2,
    maxLength: 262_146,
    pattern: HEX_PATTERN,
  });
  return parsed.ok ? { ok: true, value: parsed.value as HexData } : parsed;
};

export const parseCorrelationId: BoundaryParser<CorrelationId> = (input) => {
  const parsed = parseBoundedString(input, {
    path: '$',
    minLength: 8,
    maxLength: 128,
    pattern: CORRELATION_PATTERN,
  });
  return parsed.ok ? { ok: true, value: parsed.value as CorrelationId } : parsed;
};

export const parseUnixMilliseconds: BoundaryParser<UnixMilliseconds> = (
  input,
) => {
  if (
    typeof input !== 'number' ||
    !Number.isSafeInteger(input) ||
    input < 0
  ) {
    return issue('$', 'invalid_format', 'Timestamp must be a non-negative integer');
  }
  return { ok: true, value: input as UnixMilliseconds };
};

export function parseHttpsUrl(
  input: unknown,
  options: {
    readonly allowedOrigins?: ReadonlySet<string>;
    readonly maxLength?: number;
  } = {},
): ParseResult<URL> {
  const parsed = parseBoundedString(input, {
    path: '$',
    minLength: 1,
    maxLength: options.maxLength ?? 2048,
  });
  if (!parsed.ok) return parsed;
  let url: URL;
  try {
    url = new URL(parsed.value);
  } catch {
    return issue('$', 'invalid_format', 'URL is invalid');
  }
  if (url.protocol !== 'https:' || url.username || url.password) {
    return issue('$', 'unsupported_value', 'Only credential-free HTTPS URLs are allowed');
  }
  if (options.allowedOrigins && !options.allowedOrigins.has(url.origin)) {
    return issue('$', 'unsupported_value', 'URL origin is not allowed');
  }
  return { ok: true, value: url };
}

export function mapResult<A, B>(
  result: ParseResult<A>,
  transform: (value: A) => B,
): ParseResult<B> {
  return result.ok ? { ok: true, value: transform(result.value) } : result;
}
