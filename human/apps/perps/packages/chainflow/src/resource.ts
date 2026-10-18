// File generated from our OpenAPI spec by Scalar. See README.md for details.

import type { ChainflowDataGateway } from './client';

export abstract class APIResource {
  protected _client: ChainflowDataGateway;

  constructor(client: ChainflowDataGateway) {
    this._client = client;
  }
}
