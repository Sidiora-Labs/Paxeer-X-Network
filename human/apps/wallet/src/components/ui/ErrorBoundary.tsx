'use client';

import { Component, type ReactNode } from 'react';
import { AlertTriangle, RefreshCw } from 'lucide-react';
import { captureError } from '@/lib/observability';

interface Props {
  children: ReactNode;
  fallback?: ReactNode;
  onReset?: () => void;
}

interface State {
  hasError: boolean;
  correlationId: string;
}

export class ErrorBoundary extends Component<Props, State> {
  constructor(props: Props) {
    super(props);
    this.state = { hasError: false, correlationId: '' };
  }

  static getDerivedStateFromError(): State {
    return {
      hasError: true,
      correlationId: 'pending',
    };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    const failure = captureError(error, 'react_error_boundary', {
      componentStack: info.componentStack ?? undefined,
    });
    this.setState({ correlationId: failure.correlationId });
  }

  handleReset = () => {
    this.setState({ hasError: false, correlationId: '' });
    this.props.onReset?.();
  };

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) return this.props.fallback;

      return (
        <div className="flex flex-col items-center justify-center text-center py-16 px-6 gap-4">
          <div className="w-16 h-16 rounded-2xl bg-red-500/10 flex items-center justify-center">
            <AlertTriangle className="w-7 h-7 text-red-400" />
          </div>
          <div className="space-y-1">
            <p className="text-base font-semibold text-white/80">
              Something broke
            </p>
            <p className="text-sm text-pax-muted max-w-[260px] leading-relaxed">
              The screen could not be displayed. Retry, or share reference{' '}
              {this.state.correlationId} with support.
            </p>
          </div>
          <button
            onClick={this.handleReset}
            className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-white/[0.08] text-sm font-medium press-scale hover:bg-white/[0.12] transition-colors"
          >
            <RefreshCw className="w-4 h-4" />
            Reload
          </button>
        </div>
      );
    }

    return this.props.children;
  }
}
