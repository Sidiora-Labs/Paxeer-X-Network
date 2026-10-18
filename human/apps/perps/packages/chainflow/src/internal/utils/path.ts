import { ChainflowDataGatewayError } from '../../core/error';

/**
 * Percent-encode everything that isn't safe to have in a path without encoding safe chars.
 *
 * Taken from https://datatracker.ietf.org/doc/html/rfc3986#section-3.3:
 * > unreserved  = ALPHA / DIGIT / "-" / "." / "_" / "~"
 * > sub-delims  = "!" / "$" / "&" / "'" / "(" / ")" / "*" / "+" / "," / ";" / "="
 * > pchar       = unreserved / pct-encoded / sub-delims / ":" / "@"
 */
export function encodeURIPath(str: string) {
  return str.replace(/[^A-Za-z0-9\-._~!$&'()*+,;=:@]+/g, encodeURIComponent);
}

/**
 * Like {@link encodeURIPath} but leaves `/` unescaped, implementing RFC 6570 reserved expansion
 * (`{+var}`) for path-like parameters such as a file path. The slash is part of the route shape for
 * these params — `docs/example.txt` must stay nested under `.../files/docs/example.txt` rather than
 * collapsing to a single `docs%2Fexample.txt` segment that points at a different backend path. Each
 * `/`-separated segment is still run through {@link encodeURIPath}, so every other unsafe character is
 * percent-encoded with the exact same rules, and the tag function below still rejects `.`/`..` segments,
 * so a reserved value cannot smuggle in path traversal.
 */
export function encodeURIPathReserved(str: string) {
  return str.split('/').map(encodeURIPath).join('/');
}

/**
 * Wrapper marking a path-parameter value for reserved expansion. The {@link createPathTagFunction} tag
 * detects this instance and encodes its value with {@link encodeURIPathReserved} (slash-preserving)
 * instead of the default per-segment encoder, while leaving all other interpolated params untouched.
 */
class ReservedPathParam {
  constructor(readonly value: string) {}
  toString(): string {
    return this.value;
  }
}

/**
 * Marks a value so the `path` tag preserves `/` when encoding it (RFC 6570 reserved expansion). Used by
 * generated request paths for parameters the spec flags with `allowReserved` (e.g. file paths).
 */
export const reserved = (value: unknown): ReservedPathParam => new ReservedPathParam('' + value);

const EMPTY = /* @__PURE__ */ Object.freeze(/* @__PURE__ */ Object.create(null));

export const createPathTagFunction = (pathEncoder = encodeURIPath) =>
  function path(statics: readonly string[], ...params: readonly unknown[]): string {
    // If there are no params, no processing is needed.
    if (statics.length === 1) return statics[0]!;

    let postPath = false;
    const invalidSegments = [];
    const path = statics.reduce((previousValue, currentValue, index) => {
      if (/[?#]/.test(currentValue)) {
        postPath = true;
      }
      const param = params[index];
      // Reserved params keep `/` (file-path-like values); everything else uses the default encoder.
      const isReserved = param instanceof ReservedPathParam;
      const value = isReserved ? param.value : param;
      let encoded = (postPath ? encodeURIComponent : isReserved ? encodeURIPathReserved : pathEncoder)(
        '' + value,
      );
      if (
        index !== params.length &&
        (value == null ||
          (typeof value === 'object' &&
            // handle values from other realms
            value.toString ===
              Object.getPrototypeOf(Object.getPrototypeOf((value as any).hasOwnProperty ?? EMPTY) ?? EMPTY)
                ?.toString))
      ) {
        encoded = value + '';
        invalidSegments.push({
          start: previousValue.length + currentValue.length,
          length: encoded.length,
          error: `Value of type ${Object.prototype.toString
            .call(value)
            .slice(8, -1)} is not a valid path parameter`,
        });
      }
      return previousValue + currentValue + (index === params.length ? '' : encoded);
    }, '');

    const pathOnly = path.split(/[?#]/, 1)[0]!;
    const invalidSegmentPattern = /(?<=^|\/)(?:\.|%2e){1,2}(?=\/|$)/gi;
    let match;

    // Find all invalid segments
    while ((match = invalidSegmentPattern.exec(pathOnly)) !== null) {
      invalidSegments.push({
        start: match.index,
        length: match[0].length,
        error: `Value "${match[0]}" can\'t be safely passed as a path parameter`,
      });
    }

    invalidSegments.sort((a, b) => a.start - b.start);

    if (invalidSegments.length > 0) {
      let lastEnd = 0;
      const underline = invalidSegments.reduce((acc, segment) => {
        const spaces = ' '.repeat(segment.start - lastEnd);
        const arrows = '^'.repeat(segment.length);
        lastEnd = segment.start + segment.length;
        return acc + spaces + arrows;
      }, '');

      throw new ChainflowDataGatewayError(
        `Path parameters result in path with invalid segments:\n${invalidSegments
          .map((e) => e.error)
          .join('\n')}\n${path}\n${underline}`,
      );
    }

    return path;
  };

/**
 * URI-encodes path params and ensures no unsafe /./ or /../ path segments are introduced.
 */
export const path = /* @__PURE__ */ createPathTagFunction(encodeURIPath);
