export interface AttestorPeerState {
  reachable: boolean;
  rtt_ns: number;
}

export interface AttestorHealthReport {
  node_id: string;
  region: string;
  share_count: number;
  refresh_epoch: number;
  share_error?: string;
  audit_sequence: number;
  audit_head: string;
  peers: Record<string, AttestorPeerState>;
  reachable_peers: number;
  readiness_error?: string;
  ready: boolean;
}

export interface NodeHealth {
  endpoint: string;
  nodeId: string | null;
  healthy: boolean;
  latencyMs: number | null;
  checkedAt: number | null;
  report: AttestorHealthReport | null;
  lastError: string | null;
}

export interface QuorumMember {
  endpoint: string;
  nodeId: string;
  latencyMs: number;
}

export class QuorumUnavailableError extends Error {
  readonly healthy: number;
  readonly required: number;
  constructor(healthy: number, required: number) {
    super(`attestor quorum unavailable: ${healthy} healthy node(s), ${required} required`);
    this.name = 'QuorumUnavailableError';
    this.healthy = healthy;
    this.required = required;
  }
}

export function isHealthy(node: NodeHealth, required: number): boolean {
  if (!node.healthy || node.report === null || node.nodeId === null || node.latencyMs === null) {
    return false;
  }
  return node.report.ready && node.report.share_error === undefined && node.report.reachable_peers + 1 >= required;
}

export function selectQuorum(nodes: readonly NodeHealth[], required: number): QuorumMember[] {
  const seen = new Set<string>();
  const candidates: QuorumMember[] = [];
  for (const node of nodes) {
    if (!isHealthy(node, required)) continue;
    const nodeId = node.nodeId as string;
    if (seen.has(nodeId)) continue;
    seen.add(nodeId);
    candidates.push({ endpoint: node.endpoint, nodeId, latencyMs: node.latencyMs as number });
  }
  if (candidates.length < required) {
    throw new QuorumUnavailableError(candidates.length, required);
  }
  candidates.sort((a, b) => a.latencyMs - b.latencyMs || a.nodeId.localeCompare(b.nodeId));
  return candidates.slice(0, required);
}
