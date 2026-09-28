export interface ExternalProductDescriptor {
  readonly id: string;
  readonly label: string;
  readonly origin: string;
  readonly trust: 'first-party' | 'reviewed-third-party' | 'untrusted';
  readonly supportedCustody: readonly (
    | 'self-custody'
    | 'managed'
    | 'funded'
  )[];
  readonly handoff: 'embedded-browser' | 'system-browser';
}
