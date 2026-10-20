/**
 * Design-port Icon namespace.
 *
 * Every icon referenced by src/components/design/shared.jsx:14-28 has a
 * lucide-react equivalent (already installed at package.json:41). This file
 * re-exports them under the same `Icon.*` shape the design prototype used,
 * with the prototype's sizing and stroke defaults baked in so visual parity
 * is preserved without restating per-call.
 *
 * Phase 1 port, 2026-04-23. See DESIGN-AUDIT.md §5 (deliverable #3).
 */
import {
  ArrowRight,
  ArrowUpDown,
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  Download,
  Plus,
  QrCode,
  Send,
  X,
  Zap,
  type LucideProps,
} from "lucide-react";

type IconFn = (props?: LucideProps) => JSX.Element;

/**
 * Mirrors the Icon object from design/shared.jsx with identical default
 * sizes, stroke widths, and linecap/linejoin so ported components render
 * pixel-equivalent to the static prototype.
 */
export const Icon: Record<
  | "arrow"
  | "chevron"
  | "x"
  | "check"
  | "up"
  | "down"
  | "send"
  | "receive"
  | "swap"
  | "zap"
  | "plus"
  | "copy"
  | "qr",
  IconFn
> = {
  arrow: (p) => (
    <ArrowRight
      size={14}
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  chevron: (p) => (
    <ChevronDown size={10} strokeWidth={2.2} strokeLinecap="round" {...p} />
  ),
  x: (p) => <X size={14} strokeWidth={2} strokeLinecap="round" {...p} />,
  check: (p) => (
    <Check
      size={14}
      strokeWidth={2.4}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  up: (p) => (
    <ChevronUp size={10} strokeWidth={2.4} strokeLinecap="round" {...p} />
  ),
  down: (p) => (
    <ChevronDown size={10} strokeWidth={2.4} strokeLinecap="round" {...p} />
  ),
  send: (p) => (
    <Send
      size={14}
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  receive: (p) => (
    <Download
      size={14}
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  swap: (p) => (
    <ArrowUpDown
      size={14}
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  zap: (p) => (
    <Zap
      size={14}
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  plus: (p) => <Plus size={14} strokeWidth={2} strokeLinecap="round" {...p} />,
  copy: (p) => (
    <Copy
      size={12}
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
  qr: (p) => (
    <QrCode
      size={14}
      strokeWidth={1.6}
      strokeLinecap="round"
      strokeLinejoin="round"
      {...p}
    />
  ),
};
