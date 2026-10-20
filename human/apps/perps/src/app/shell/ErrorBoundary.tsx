import React from "react";
import { ErrorPanel } from "@/widgets/errors/ErrorPanel";
import { classifyError } from "@/core/errors/classify";
import { reportError } from "@/core/errors/report";
import type { UserFacingError } from "@/core/errors/types";

interface Props {
  children: React.ReactNode;
  fallbackLabel?: string;
  /**
   * Compact mode for narrow widget slots (orderbook, watchlist).
   * Tightens padding without losing the title/fix/retry/report anatomy.
   */
  size?: "sm" | "md";
}

interface State {
  hasError: boolean;
  error: UserFacingError | null;
  remountKey: number;
}

/**
 * Component-scoped error boundary. Renders the brand `ErrorPanel`
 * with a classified `UserFacingError` so users never see a raw
 * `error.message`, and always get a fix suggestion + a Report button
 * for unknowns. Children are fully remounted on retry so any stale
 * downstream state is cleared.
 */
export class ErrorBoundary extends React.Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, error: null, remountKey: 0 };
  }

  static getDerivedStateFromError(error: Error): Partial<State> {
    const ufe = classifyError(error, "boundary");
    return { hasError: true, error: ufe };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    const label = this.props.fallbackLabel ?? "Component";
    const ufe = classifyError(error, `boundary:${label}`);
    void reportError(ufe, {
      extra: { componentStack: info.componentStack ?? null },
      tags: { boundary_label: label },
    });
  }

  handleRetry = () => {
    // Bump remountKey so children are fully unmounted+remounted on retry,
    // not just reset in place. This clears any downstream stale state that
    // may have caused the error.
    this.setState((prev) => ({
      hasError: false,
      error: null,
      remountKey: prev.remountKey + 1,
    }));
  };

  render() {
    if (this.state.hasError && this.state.error) {
      return (
        <ErrorPanel
          error={this.state.error}
          onRetry={this.handleRetry}
          size={this.props.size ?? "sm"}
        />
      );
    }

    return (
      <React.Fragment key={this.state.remountKey}>
        {this.props.children}
      </React.Fragment>
    );
  }
}
