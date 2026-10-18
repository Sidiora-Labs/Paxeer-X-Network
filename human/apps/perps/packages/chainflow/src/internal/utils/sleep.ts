// File generated from our OpenAPI spec by Scalar. See README.md for details.

export const sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms));
