export interface ExternalProductDescriptor {
  readonly id: string;
  readonly label: string;
  readonly origin: string;
  readonly trust: 'first-party' | 'reviewed-third-party' | 'untrusted';
  readonly supportedCustody: readonly ('managed' | 'funded')[];
  readonly handoff: 'system-browser';
}
