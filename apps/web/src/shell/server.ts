import {
  SHELL_HINT_COOKIE,
  ShellSelector,
  type PointerCapability,
  type ShellSelection,
} from "./selector.ts";

interface RequestCookies {
  get(name: string): { readonly value: string } | undefined;
}

function viewportWidth(value: string | null): number | undefined {
  if (value === null || !/^\d{1,6}$/u.test(value)) {
    return undefined;
  }
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 && parsed <= 100_000 ? parsed : undefined;
}

function pointerCapability(value: string | undefined): PointerCapability | undefined {
  const match = /^v1\.\d{1,6}\.(coarse|fine|none)$/u.exec(value ?? "");
  return match?.[1] as PointerCapability | undefined;
}

function cookieViewportWidth(value: string | undefined): number | undefined {
  const match = /^v1\.(\d{1,6})\.(?:coarse|fine|none)$/u.exec(value ?? "");
  return viewportWidth(match?.[1] ?? null);
}

export function selectServerShell(headers: Headers, cookies: RequestCookies): ShellSelection {
  const cookie = cookies.get(SHELL_HINT_COOKIE)?.value;
  const hintedWidth = viewportWidth(
    headers.get("sec-ch-viewport-width") ?? headers.get("viewport-width"),
  );
  const width = hintedWidth ?? cookieViewportWidth(cookie);
  const pointer = pointerCapability(cookie);
  return ShellSelector.server({
    ...(width === undefined ? {} : { viewportWidth: width }),
    ...(pointer === undefined ? {} : { pointer }),
  });
}
