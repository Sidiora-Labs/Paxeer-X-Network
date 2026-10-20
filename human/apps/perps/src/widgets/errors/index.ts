/* ══════════════════════════════════════════════════════════════════════
   Error widgets — public surface
   ══════════════════════════════════════════════════════════════════════ */

export { showErrorToast, toastError } from "./ErrorToast";
export type { ShowErrorToastOptions, ToastErrorOptions } from "./ErrorToast";
export { ErrorPanel } from "./ErrorPanel";
export {
  ReportFeedbackModalHost,
  openReportModal,
  closeReportModal,
} from "./ReportFeedbackModal";
export { ToastAction } from "./ToastActionBridge";
export type { ToastActionElement } from "./ToastActionBridge";
