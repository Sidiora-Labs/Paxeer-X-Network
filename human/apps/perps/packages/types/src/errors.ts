export class ApiError extends Error {
  constructor(message: string, private readonly code: number) {
    super(message);
    this.name = "ApiError";
  }
}

export class SDKError extends Error {
  constructor(message: string) {
    super(`[PAXEER_NETWORK SDK ERROR]:${message}`);
    this.name = "SDKError";
  }
}
