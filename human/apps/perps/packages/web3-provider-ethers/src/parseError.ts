import { DecodedError, ErrorDecoder } from "ethers-decode-error";

const errorDecoder = ErrorDecoder.create();

type ParsedError = DecodedError & {
  message: string;
};

export async function parseError(rawError: any): Promise<ParsedError> {
  const error: DecodedError = await errorDecoder.decode(rawError);
  const reason = error.reason ?? "";
  console.error("parsedError", error);

  return {
    ...error,
    message: replacePrefix(reason),
  };
}

function replacePrefix(reason: string) {
  const prefixes = ["ethers-user-denied: ", "ethers-unsupported: "];

  if (typeof reason !== "string") {
    return reason;
  }

  for (const prefix of prefixes) {
    if (reason.startsWith(prefix)) {
      return reason.replace(prefix, "");
    }
  }

  return reason;
}
