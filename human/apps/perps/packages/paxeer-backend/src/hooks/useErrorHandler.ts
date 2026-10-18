/* ══════════════════════════════════════════════════════════════════════
   useErrorHandler

   Ergonomic React hook for wiring catch sites into the error system.

     const { handle, wrap } = useErrorHandler({ context: "order-form" });

     try {
       await placeOrder(...);
     } catch (e) {
       handle(e, { onRetry: () => placeOrder(...) });
     }

     // or — wrap any async function so it handles errors automatically:
     const safePlace = wrap(placeOrder, { context: "place-order" });
     await safePlace(...);

   Both `handle` and `wrap` route through `classifyError` → `showErrorToast`
   → `reportError` so callers never touch raw error.message strings.
   ══════════════════════════════════════════════════════════════════════ */

import { useCallback, useMemo } from "react";
import { classifyError, isUserFacingError } from "@paxeer-network/paxeer-backend/core/errors/classify";
import { reportError } from "@paxeer-network/paxeer-backend/core/errors/report";
import type { ErrorContext, HandleErrorOptions, UserFacingError } from "@paxeer-network/paxeer-backend/core/errors/types";
import { showErrorToast } from "@paxeer-network/paxeer-backend/errors/ErrorToast";

export interface UseErrorHandlerOptions {
  /** Default context tag applied to every error this hook handles. */
  context?: ErrorContext;
}

export interface ErrorHandlerApi {
  /** Classify, toast, and report a thrown thing. Returns the classified error. */
  handle: (err: unknown, opts?: HandleErrorOptions) => UserFacingError;
  /** Wrap an async function so all throws route through `handle`. */
  wrap: <Args extends unknown[], R>(
    fn: (...args: Args) => Promise<R>,
    opts?: HandleErrorOptions,
  ) => (...args: Args) => Promise<R | undefined>;
}

export function useErrorHandler(defaults: UseErrorHandlerOptions = {}): ErrorHandlerApi {
  const handle = useCallback(
    (err: unknown, opts: HandleErrorOptions = {}): UserFacingError => {
      const ctx = opts.context ?? defaults.context;
      const ufe = isUserFacingError(err) ? err : classifyError(err, ctx);

      if (opts.showToast !== false) {
        showErrorToast(ufe);
      }

      // Report runs asynchronously and never throws back at the caller.
      void reportError(ufe, {
        context: ctx,
        feedback: opts.feedback,
        extra: opts.extra,
        tags: opts.tags,
      });

      return ufe;
    },
    [defaults.context],
  );

  const wrap = useCallback(
    <Args extends unknown[], R>(
      fn: (...args: Args) => Promise<R>,
      opts: HandleErrorOptions = {},
    ): ((...args: Args) => Promise<R | undefined>) => {
      return async (...args: Args) => {
        try {
          return await fn(...args);
        } catch (e) {
          handle(e, opts);
          return undefined;
        }
      };
    },
    [handle],
  );

  return useMemo(() => ({ handle, wrap }), [handle, wrap]);
}
