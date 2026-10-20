const GATEWAY_ENV = "NEXT_PUBLIC_PERPS_GATEWAY_URL";

export interface GatewayUrls {
  readonly origin: string;
  readonly restApiUrl: string;
  readonly spotApiBase: string;
  readonly perfdbOrigin: string;
  readonly graphql: { readonly url: string; readonly wsUrl: string };
  readonly venueWsUrl: string;
  readonly spotUserWsUrl: string;
}

function parseGatewayOrigin(raw: string | undefined): URL {
  if (!raw) throw new Error(`${GATEWAY_ENV} is not set`);
  let url: URL;
  try {
    url = new URL(raw);
  } catch {
    throw new Error(`${GATEWAY_ENV} is not a valid URL: ${raw}`);
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    throw new Error(`${GATEWAY_ENV} must use http or https: ${raw}`);
  }
  if (url.username || url.password || url.pathname !== "/" || url.search || url.hash) {
    throw new Error(`${GATEWAY_ENV} must be a bare origin: ${raw}`);
  }
  return url;
}

const gatewayOrigin = parseGatewayOrigin(process.env.NEXT_PUBLIC_PERPS_GATEWAY_URL);
const origin = gatewayOrigin.origin;
const wsOrigin = `${gatewayOrigin.protocol === "https:" ? "wss:" : "ws:"}//${gatewayOrigin.host}`;

const GATEWAY_URLS: GatewayUrls = Object.freeze({
  origin,
  restApiUrl: `${origin}/api`,
  spotApiBase: origin,
  perfdbOrigin: origin,
  graphql: Object.freeze({ url: `${origin}/graphql`, wsUrl: `${wsOrigin}/graphql` }),
  venueWsUrl: `${wsOrigin}/ws`,
  spotUserWsUrl: `${wsOrigin}/v1/ws`,
});

export function gatewayUrls(): GatewayUrls {
  return GATEWAY_URLS;
}
