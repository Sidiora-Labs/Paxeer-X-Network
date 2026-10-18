export class ApiError extends Error {
  constructor(message: string, private readonly code: number) {
    super(message);
    this.name = "ApiError";
  }
}
