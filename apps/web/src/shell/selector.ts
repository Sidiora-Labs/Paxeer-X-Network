export const TABLET_MIN_WIDTH = 768;
export const SHELL_HINT_COOKIE = "__Host-layerx-shell";

export type Shell = "mobile" | "desktop";
export type PointerCapability = "coarse" | "fine" | "none";

export interface ShellHints {
  readonly viewportWidth?: number;
  readonly pointer?: PointerCapability;
}

export interface ShellSelection {
  readonly shell: Shell;
  readonly pointer: PointerCapability;
  readonly viewportWidth?: number;
  readonly touchTargets: boolean;
  readonly source: "server" | "client";
}

export interface ShellConfirmation {
  readonly selection: ShellSelection;
  readonly correction: "none" | "pre-paint";
}

export interface DeepLinkResolution {
  readonly href: string;
  readonly shell: Shell;
}

function validViewportWidth(value: number | undefined): value is number {
  return value !== undefined && Number.isInteger(value) && value > 0 && value <= 100_000;
}

function normalizedPointer(pointer: PointerCapability | undefined): PointerCapability {
  return pointer ?? "none";
}

function sameSelection(left: ShellSelection, right: ShellSelection): boolean {
  return left.shell === right.shell && left.pointer === right.pointer && left.touchTargets === right.touchTargets;
}

function selection(hints: ShellHints, source: ShellSelection["source"]): ShellSelection {
  const viewportWidth = validViewportWidth(hints.viewportWidth) ? hints.viewportWidth : undefined;
  const pointer = normalizedPointer(hints.pointer);
  const shell = viewportWidth !== undefined && viewportWidth < TABLET_MIN_WIDTH ? "mobile" : "desktop";
  const touchTargets = shell === "mobile" || pointer === "coarse";

  return {
    shell,
    pointer,
    ...(viewportWidth === undefined ? {} : { viewportWidth }),
    touchTargets,
    source,
  };
}

function normalizedAppHref(href: string): string {
  const base = new URL("https://layerx.invalid");
  const resolved = new URL(href, base);
  if (
    resolved.origin !== base.origin ||
    (resolved.pathname !== "/app" && !resolved.pathname.startsWith("/app/"))
  ) {
    throw new TypeError("LayerX app deep links must remain within the authenticated plane");
  }
  return `${resolved.pathname}${resolved.search}${resolved.hash}`;
}

export class ShellSelector {
  private readonly source: ShellSelection["source"];

  private constructor(source: ShellSelection["source"]) {
    this.source = source;
  }

  private select(hints: ShellHints): ShellSelection {
    return selection(hints, this.source);
  }

  static server(hints: ShellHints): ShellSelection {
    return new ShellSelector("server").select(hints);
  }

  static client(hints: ShellHints): ShellSelection {
    if (!validViewportWidth(hints.viewportWidth)) {
      throw new TypeError("Client shell selection requires a valid viewport width");
    }
    return new ShellSelector("client").select(hints);
  }

  static confirm(server: ShellSelection, hints: ShellHints): ShellConfirmation {
    const confirmed = ShellSelector.client(hints);
    return {
      selection: confirmed,
      correction: sameSelection(server, confirmed) ? "none" : "pre-paint",
    };
  }

  static resolveDeepLink(href: string, shell: Shell): DeepLinkResolution {
    return { href: normalizedAppHref(href), shell };
  }
}
