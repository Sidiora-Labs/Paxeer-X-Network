// File generated from our OpenAPI spec by Scalar. See README.md for details.

import * as qs from '../qs/stringify';

export function stringifyQuery(query: object | Record<string, unknown>): string {
  // Substituted from the same config as the client's own `stringifyQuery`; this module is re-exported
  // through the package's `./*` subpath, so a hardcoded format here would serialize arrays differently
  // from every generated request.
  return qs.stringify(query, { arrayFormat: 'comma', allowDots: false });
}
