import {
  issue,
  parseBoundedString,
  parseCorrelationId,
  parseRecord,
  rejectUnknownFields,
  type BoundaryParser,
  type CorrelationId,
} from '../shared';

export interface ServerRequestMeta {
  readonly method: 'DELETE' | 'GET' | 'HEAD' | 'POST' | 'PUT';
  readonly contentType?: string;
  readonly contentLength: number;
  readonly correlationId: CorrelationId;
}

export const parseServerRequestMeta: BoundaryParser<ServerRequestMeta> = (
  input,
) => {
  const record = parseRecord(input);
  if (!record.ok) return record;
  const fields = rejectUnknownFields(
    record.value,
    new Set(['method', 'contentType', 'contentLength', 'correlationId']),
  );
  if (!fields.ok) return fields;
  const methods = new Set(['DELETE', 'GET', 'HEAD', 'POST', 'PUT']);
  if (typeof record.value.method !== 'string' || !methods.has(record.value.method)) {
    return issue('$.method', 'unsupported_value', 'HTTP method is unsupported');
  }
  if (
    typeof record.value.contentLength !== 'number' ||
    !Number.isSafeInteger(record.value.contentLength) ||
    record.value.contentLength < 0 ||
    record.value.contentLength > 1_048_576
  ) {
    return issue('$.contentLength', 'out_of_bounds', 'Content length is invalid');
  }
  let contentType: string | undefined;
  if (record.value.contentType !== undefined) {
    const parsed = parseBoundedString(record.value.contentType, {
      path: '$.contentType',
      minLength: 1,
      maxLength: 128,
    });
    if (!parsed.ok) return parsed;
    contentType = parsed.value.toLowerCase();
  }
  const correlationId = parseCorrelationId(record.value.correlationId);
  if (!correlationId.ok) return correlationId;
  return {
    ok: true,
    value: {
      method: record.value.method as ServerRequestMeta['method'],
      contentType,
      contentLength: record.value.contentLength,
      correlationId: correlationId.value,
    },
  };
};

export interface UpstreamEnvelope<T> {
  readonly status: number;
  readonly contentType: string;
  readonly body: T;
}

export function upstreamEnvelopeParser<T>(
  bodyParser: BoundaryParser<T>,
): BoundaryParser<UpstreamEnvelope<T>> {
  return (input) => {
    const record = parseRecord(input);
    if (!record.ok) return record;
    const fields = rejectUnknownFields(
      record.value,
      new Set(['status', 'contentType', 'body']),
    );
    if (!fields.ok) return fields;
    if (
      typeof record.value.status !== 'number' ||
      !Number.isInteger(record.value.status) ||
      record.value.status < 100 ||
      record.value.status > 599
    ) {
      return issue('$.status', 'out_of_bounds', 'Upstream status is invalid');
    }
    const contentType = parseBoundedString(record.value.contentType, {
      path: '$.contentType',
      minLength: 1,
      maxLength: 128,
    });
    if (!contentType.ok) return contentType;
    const body = bodyParser(record.value.body);
    if (!body.ok) return body;
    return {
      ok: true,
      value: {
        status: record.value.status,
        contentType: contentType.value.toLowerCase(),
        body: body.value,
      },
    };
  };
}
