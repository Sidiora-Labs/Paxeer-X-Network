// File generated from our OpenAPI spec by Scalar. See README.md for details.

import type { ChainflowDataGateway } from './client';
import type { FinalRequestOptions } from './internal/request-options';

export type APIResponseProps = {
  readonly response: Response;
  readonly options: FinalRequestOptions;
  readonly controller: AbortController;
  readonly requestLogID?: string | undefined;
  readonly retryOfRequestLogID?: string | undefined;
  readonly startTime?: number | undefined;
};

export type ParseResponse<T> = (client: ChainflowDataGateway, props: APIResponseProps) => T | Promise<T>;

export const defaultParseResponse = async <T>(_client: unknown, props: APIResponseProps): Promise<T> => {
  const { response } = props;
  if (response.status === 204) return null as T;
  if (props.options.__binaryResponse) return response as T;
  const contentType = response.headers.get('content-type');
  const mediaType = contentType?.split(';')[0]?.trim();
  const isJson = mediaType?.includes('application/json') || mediaType?.endsWith('+json');
  if (isJson && response.headers.get('content-length') === '0') return undefined as T;
  if (isJson) return (await response.json()) as T;
  return (await response.text()) as unknown as T;
};

/** A Promise subclass providing SDK response helpers. */
export class APIPromise<T> extends Promise<T> {
  private parsedPromise: Promise<T> | undefined;

  constructor(
    private readonly client: ChainflowDataGateway,
    private readonly responsePromise: Promise<APIResponseProps>,
    private readonly parseResponse: ParseResponse<T> = defaultParseResponse,
  ) {
    super((resolve) => {
      resolve(undefined as T);
    });
  }

  _thenUnwrap<U>(transform: (data: T, props: APIResponseProps) => U): APIPromise<U> {
    return new APIPromise(this.client, this.responsePromise, async (client, props) =>
      transform(await this.parseResponse(client, props), props),
    );
  }

  asResponse(): Promise<Response> {
    return this.responsePromise.then((props) => props.response);
  }

  async withResponse(): Promise<{ data: T; response: Response }> {
    const [data, response] = await Promise.all([this.parse(), this.asResponse()]);
    return { data, response };
  }

  private parse(): Promise<T> {
    if (!this.parsedPromise) {
      this.parsedPromise = this.responsePromise.then((props) => this.parseResponse(this.client, props));
    }
    return this.parsedPromise;
  }

  override then<TResult1 = T, TResult2 = never>(
    onfulfilled?: ((value: T) => TResult1 | PromiseLike<TResult1>) | undefined | null,
    onrejected?: ((reason: unknown) => TResult2 | PromiseLike<TResult2>) | undefined | null,
  ): Promise<TResult1 | TResult2> {
    return this.parse().then(onfulfilled, onrejected);
  }

  override catch<TResult = never>(
    onrejected?: ((reason: unknown) => TResult | PromiseLike<TResult>) | undefined | null,
  ): Promise<T | TResult> {
    return this.parse().catch(onrejected);
  }

  override finally(onfinally?: (() => void) | undefined | null): Promise<T> {
    return this.parse().finally(onfinally);
  }
}
