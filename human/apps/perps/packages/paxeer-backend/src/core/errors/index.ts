/* ══════════════════════════════════════════════════════════════════════
   Error system — public surface

   Import from this barrel from the rest of the app:

     import {
       classifyError,
       makeError,
       reportError,
       leaveBreadcrumb,
       isUserFacingError,
     } from "@paxeer-network/paxeer-backend/core/errors";

     import type { UserFacingError, ErrorCode } from "@paxeer-network/paxeer-backend/core/errors";
   ══════════════════════════════════════════════════════════════════════ */

export { classifyError, makeError, isUserFacingError } from "./classify";
export { reportError, leaveBreadcrumb } from "./report";
export { ERROR_CATALOG, getCatalogEntry } from "./catalog";
export { installGlobalErrorHandlers } from "./global";

export type {
  ErrorCode,
  ErrorContext,
  ErrorSeverity,
  UserFacingError,
  ReportErrorOptions,
  HandleErrorOptions,
} from "./types";
