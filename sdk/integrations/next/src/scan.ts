export interface BundleArtifact {
  readonly path: string;
  readonly bytes: Uint8Array;
}

export type BundleFindingKind =
  | "declared-secret-value"
  | "declared-secret-name"
  | "private-key-block"
  | "published-key-material";

export interface BundleFinding {
  readonly path: string;
  readonly kind: BundleFindingKind;
  readonly locator: string;
  readonly offset: number;
}

export interface BundleScanRequest {
  readonly artifacts: readonly BundleArtifact[];
  readonly secretValues: readonly string[];
  readonly secretNames: readonly string[];
}

const PRIVATE_KEY_BLOCK = /-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----/gu;
const PUBLISHED_KEY_MATERIAL =
  /\b(?:NEXT_PUBLIC|PUBLIC|VITE|REACT_APP|EXPO_PUBLIC)_[A-Z0-9_]*(?:TOKEN|SECRET|PRIVATE|CREDENTIAL|PASSWORD|API_KEY|SIGNING_KEY)[A-Z0-9_]*\b/gu;
const MINIMUM_SECRET_LENGTH = 8;

export function scanBundleArtifacts(request: BundleScanRequest): readonly BundleFinding[] {
  const decoder = new TextDecoder("utf-8", { fatal: false });
  const secretValues = request.secretValues.filter((value) => value.length >= MINIMUM_SECRET_LENGTH);
  const secretNames = request.secretNames.filter((value) => value.length > 0);
  const findings: BundleFinding[] = [];
  for (const artifact of request.artifacts) {
    const text = decoder.decode(artifact.bytes);
    for (const secret of secretValues) {
      const offset = text.indexOf(secret);
      if (offset >= 0) {
        findings.push({
          path: artifact.path,
          kind: "declared-secret-value",
          locator: redacted(secret.length),
          offset,
        });
      }
    }
    for (const name of secretNames) {
      const offset = text.indexOf(name);
      if (offset >= 0) {
        findings.push({ path: artifact.path, kind: "declared-secret-name", locator: name, offset });
      }
    }
    for (const match of text.matchAll(PRIVATE_KEY_BLOCK)) {
      findings.push({
        path: artifact.path,
        kind: "private-key-block",
        locator: match[0] ?? "",
        offset: match.index ?? 0,
      });
    }
    for (const match of text.matchAll(PUBLISHED_KEY_MATERIAL)) {
      findings.push({
        path: artifact.path,
        kind: "published-key-material",
        locator: match[0] ?? "",
        offset: match.index ?? 0,
      });
    }
  }
  return findings;
}

export function collectSecretValues(environment: Readonly<Record<string, string | undefined>>): readonly string[] {
  const values: string[] = [];
  for (const [name, value] of Object.entries(environment)) {
    if (value === undefined || value.length < MINIMUM_SECRET_LENGTH) {
      continue;
    }
    if (isSecretName(name)) {
      values.push(value);
    }
  }
  return values;
}

export function collectSecretNames(environment: Readonly<Record<string, string | undefined>>): readonly string[] {
  return Object.keys(environment).filter((name) => isSecretName(name));
}

export function isSecretName(name: string): boolean {
  return /(^|_)(TOKEN|SECRET|PRIVATE|CREDENTIAL|PASSWORD|SIGNING_KEY|API_KEY|SEED|MNEMONIC)(_|$)/u.test(name);
}

export function bundleScanReport(findings: readonly BundleFinding[]): string {
  return JSON.stringify({
    scanned: "browser-facing-artifacts",
    findings,
    passed: findings.length === 0,
  });
}

function redacted(length: number): string {
  return `[REDACTED:${String(length)}]`;
}
