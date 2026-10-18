'use client';
import { Component, type ReactNode } from 'react';
import { recoveryError, requestDraftHandoff, retryDelay, type RecoveryError } from '@/core/app-foundation/recovery';
import { reportRecovery } from '@/core/app-foundation/redacted-report';
interface Props { children: ReactNode; fallback?: ReactNode; area?: string; onRetry?: () => void | Promise<void>; onReconnect?: () => void; preserveDraft?: () => void | Promise<void> }
interface State { error: RecoveryError | null; attempts: number; retrying: boolean }
export class ErrorBoundary extends Component<Props, State> {
  state: State = { error: null, attempts: 0, retrying: false };
  private active = false;
  private reloading = false;
  componentDidMount() { this.active = true; }
  private timer: ReturnType<typeof setTimeout> | undefined;
  static getDerivedStateFromError(error: unknown): Partial<State> { return { error: recoveryError(error), retrying: false }; }
  componentDidCatch() { if (this.state.error) reportRecovery(this.state.error, this.props.area || 'widget'); }
  componentWillUnmount() { this.active = false; clearTimeout(this.timer); }
  private retry = () => {
    if (!this.state.error?.retriable || this.state.attempts >= 3 || this.state.retrying) return;
    const attempt = this.state.attempts + 1;
    this.setState({ attempts: attempt, retrying: true });
    this.timer = setTimeout(() => {
      void Promise.resolve().then(() => this.props.onRetry?.()).then(() => { if (this.active) this.setState({ error: null, retrying: false }); }).catch(error => { if (this.active) { const failure = recoveryError(error); reportRecovery(failure, this.props.area || 'widget'); this.setState({ error: failure, retrying: false }); } });
    }, retryDelay(attempt));
  };
  private reload = async () => {
    if (this.reloading || !this.active) return;
    this.reloading = true;
    this.setState({ retrying: true });
    try { await requestDraftHandoff(); await this.props.preserveDraft?.(); if (this.active) window.location.reload(); }
    catch (error) { if (this.active) { const failure = recoveryError(error); reportRecovery(failure, this.props.area || 'widget'); this.setState({ error: failure, retrying: false }); } }
    finally { this.reloading = false; }
  };
  render() {
    const error = this.state.error;
    if (!error) return this.props.children;
    if (this.props.fallback) return this.props.fallback;
    return <section role="alert" className="rounded-xl bg-[var(--foundation-surface)] p-4 text-[var(--foundation-text)]">
      <p>{error.code === 'unauthorized' ? 'Connect your wallet to see this' : error.code === 'chunk_load' ? 'A new version is out, reload to continue' : 'Something went wrong, try again'}</p>
      {error.code === 'chunk_load' ? <button disabled={this.state.retrying} onClick={() => void this.reload()} className="mt-2 rounded-lg px-3 py-2">Reload</button> : error.code === 'unauthorized' ? <button onClick={this.props.onReconnect} disabled={!this.props.onReconnect} className="mt-2 rounded-lg px-3 py-2">Connect wallet</button> : <button onClick={this.retry} disabled={this.state.retrying || this.state.attempts >= 3} className="mt-2 rounded-lg px-3 py-2 disabled:opacity-50">{this.state.retrying ? 'Trying again...' : this.state.attempts >= 3 ? 'Still not working, reload the page' : 'Try again'}</button>}
    </section>;
  }
}
export default ErrorBoundary;
