import { KernelAvailability } from './kernel.js';
import { isRecord } from './rpc.js';
import type {
  EvidenceClass,
  EvidenceRef,
  HumanErrorBody,
  HumanMoney,
  IntentDomain,
  IntentEndpoint,
  IntentEndpointKind,
  IntentLeg,
  IntentPlan,
  IntentSigningRequirement,
  IntentSubmission,
  Journey,
  JourneyKind,
  JourneyStage,
  JourneyState,
  KernelRead,
  PlanIntentRequest,
  SubmitPlanRequest,
  VerificationLevel,
} from './types.js';

export const JOURNEY_STATES: readonly JourneyState[] = [
  'getting-ready',
  'sending',
  'processing',
  'done',
  'done-finalised',
  'still-checking',
  'refused',
  'waiting-for-you',
];

const JOURNEY_KINDS: readonly JourneyKind[] = [
  'onboarding',
  'wallet-binding',
  'deposit',
  'withdraw',
  'exit',
  'move',
  'agent-create',
  'agent-fund',
  'agent-pause',
  'agent-retire',
];

const ENDPOINT_KINDS: readonly IntentEndpointKind[] = ['paxeer-wallet', 'human', 'agent', 'agent-budget'];
const DOMAINS: readonly IntentDomain[] = ['paxeer', 'layerx'];
const EVIDENCE_CLASSES: readonly EvidenceClass[] = [
  'local-journey-state',
  'submission-record',
  'layerx-receipt',
  'checkpoint-proof',
  'paxeer-finality',
  'typed-refusal',
  'approval-hold',
  'wallet-ack',
];
const VERIFICATION_LEVELS: readonly VerificationLevel[] = [
  'unverified',
  'receipt-verified',
  'checkpoint-finalised',
  'paxeer-finalised',
];
const RETRIES: readonly HumanErrorBody['retry'][] = ['retriable', 'retriable-after', 'structural', 'final'];
const HEX32 = /^[0-9a-f]{64}$/;
const AMOUNT = /^(0|[1-9][0-9]*)$/;
const IDEMPOTENCY_KEY = /^[\x21-\x7e]{1,128}$/;
const JOURNEY_ID = /^[A-Za-z0-9_-]{1,128}$/;

export class HumanServiceError extends Error {
  readonly status: number;
  readonly code: string;
  readonly copyKey: string;
  readonly retry: HumanErrorBody['retry'];
  readonly trace: string;
  readonly body: HumanErrorBody;

  constructor(status: number, body: HumanErrorBody, trace: string) {
    super(`${body.code} (${status})`);
    this.name = 'HumanServiceError';
    this.status = status;
    this.code = body.code;
    this.copyKey = body.copy_key;
    this.retry = body.retry;
    this.trace = trace;
    this.body = body;
  }
}

export class HumanDecodeError extends Error {
  constructor(what: string) {
    super(`malformed ${what}`);
    this.name = 'HumanDecodeError';
  }
}

export interface HumanClientOptions {
  url: string;
  kernel: KernelAvailability;
  fetch?: typeof fetch;
  authorization?: () => Promise<string | null> | string | null;
}

export class HumanClient {
  private readonly base: string;
  private readonly kernel: KernelAvailability;
  private readonly fetchImpl: typeof fetch;
  private readonly authorization: HumanClientOptions['authorization'];

  constructor(options: HumanClientOptions) {
    if (!options.url) throw new Error('HumanClient: url required');
    this.base = options.url.replace(/\/$/, '');
    this.kernel = options.kernel;
    this.fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.authorization = options.authorization;
  }

  async planIntent(request: PlanIntentRequest): Promise<IntentPlan> {
    return decodeIntentPlan(await this.send('POST', '/v1/intents/plan', request, undefined));
  }

  async submitPlan(request: SubmitPlanRequest, idempotencyKey: string): Promise<KernelRead<IntentSubmission>> {
    if (!IDEMPOTENCY_KEY.test(idempotencyKey)) throw new RangeError('idempotency key is malformed');
    if (request.bindings.length === 0) throw new RangeError('a submission carries one binding per leg');
    const state = await this.kernel.current();
    if (!state.available) return state;
    const result = await this.send('POST', '/v1/intents/submit', request, idempotencyKey);
    return { available: true, result: decodeIntentSubmission(result) };
  }

  async getJourney(journeyId: string): Promise<Journey> {
    if (!JOURNEY_ID.test(journeyId)) throw new RangeError('journey id is malformed');
    return decodeJourney(await this.send('GET', `/v1/journeys/${journeyId}`, undefined, undefined));
  }

  private async send(
    method: 'GET' | 'POST',
    path: string,
    body: unknown,
    idempotencyKey: string | undefined,
  ): Promise<unknown> {
    const headers: Record<string, string> = { accept: 'application/json' };
    if (body !== undefined) headers['content-type'] = 'application/json';
    if (idempotencyKey !== undefined) headers['idempotency-key'] = idempotencyKey;
    if (this.authorization !== undefined) {
      const token = await this.authorization();
      if (token !== null) headers.authorization = `Bearer ${token}`;
    }
    const response = await this.fetchImpl(`${this.base}${path}`, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    let payload: unknown;
    try {
      payload = JSON.parse(await response.text());
    } catch {
      throw new HumanDecodeError(`response envelope (HTTP ${response.status})`);
    }
    return decodeEnvelope(response.status, payload);
  }
}

export function decodeEnvelope(status: number, value: unknown): unknown {
  const envelope = record(value, 'response envelope');
  if (typeof envelope.trace !== 'string' || envelope.trace.length === 0) throw new HumanDecodeError('envelope trace');
  if (envelope.ok === true) {
    if (status < 200 || status >= 300 || !('result' in envelope)) throw new HumanDecodeError('success envelope');
    return envelope.result;
  }
  if (envelope.ok !== false || status < 400 || status >= 600) throw new HumanDecodeError('failure envelope');
  throw new HumanServiceError(status, decodeErrorBody(envelope.error), envelope.trace);
}

function decodeErrorBody(value: unknown): HumanErrorBody {
  const error = record(value, 'error body');
  if (typeof error.code !== 'string' || typeof error.copy_key !== 'string') throw new HumanDecodeError('error body');
  if (typeof error.retry !== 'string' || !(RETRIES as readonly string[]).includes(error.retry)) {
    throw new HumanDecodeError('error retry');
  }
  const body: HumanErrorBody = { code: error.code, copy_key: error.copy_key, retry: error.retry as HumanErrorBody['retry'] };
  if ('retry_after_ms' in error) {
    if (typeof error.retry_after_ms !== 'number' || !Number.isInteger(error.retry_after_ms) || error.retry_after_ms <= 0) {
      throw new HumanDecodeError('error retry_after_ms');
    }
    body.retry_after_ms = error.retry_after_ms;
  }
  if ((body.retry === 'retriable-after') !== (body.retry_after_ms !== undefined)) throw new HumanDecodeError('error retry_after_ms');
  if ('field' in error) {
    if (typeof error.field !== 'string') throw new HumanDecodeError('error field');
    body.field = error.field;
  }
  return body;
}

function record(value: unknown, what: string): Record<string, unknown> {
  if (!isRecord(value)) throw new HumanDecodeError(what);
  return value;
}

function text(value: unknown, what: string): string {
  if (typeof value !== 'string' || value.length === 0) throw new HumanDecodeError(what);
  return value;
}

function oneOf<T extends string>(value: unknown, allowed: readonly T[], what: string): T {
  if (typeof value !== 'string' || !(allowed as readonly string[]).includes(value)) throw new HumanDecodeError(what);
  return value as T;
}

function hex32(value: unknown, what: string): string {
  if (typeof value !== 'string' || !HEX32.test(value)) throw new HumanDecodeError(what);
  return value;
}

function index(value: unknown, what: string): number {
  if (typeof value !== 'number' || !Number.isInteger(value) || value < 0) throw new HumanDecodeError(what);
  return value;
}

function money(value: unknown, what: string): HumanMoney {
  const doc = record(value, what);
  if (typeof doc.amount !== 'string' || !AMOUNT.test(doc.amount)) throw new HumanDecodeError(`${what} amount`);
  return { amount: doc.amount, currency: text(doc.currency, `${what} currency`) };
}

function endpoint(value: unknown, what: string): IntentEndpoint {
  const doc = record(value, what);
  const kind = oneOf(doc.kind, ENDPOINT_KINDS, `${what} kind`);
  if (kind === 'paxeer-wallet') {
    if ('account' in doc) throw new HumanDecodeError(`${what} account`);
    return { kind };
  }
  return { kind, account: text(doc.account, `${what} account`) };
}

export function decodeIntentPlan(value: unknown): IntentPlan {
  const plan = record(value, 'intent plan');
  if (!Array.isArray(plan.legs) || plan.legs.length === 0) throw new HumanDecodeError('intent plan legs');
  const legs: IntentLeg[] = plan.legs.map((entry, position) => {
    const leg = record(entry, 'intent leg');
    const legIndex = index(leg.index, 'intent leg index');
    if (legIndex !== position) throw new HumanDecodeError('intent leg order');
    return {
      index: legIndex,
      mechanism: text(leg.mechanism, 'intent leg mechanism'),
      domain: oneOf(leg.domain, DOMAINS, 'intent leg domain'),
      source: endpoint(leg.source, 'intent leg source'),
      destination: endpoint(leg.destination, 'intent leg destination'),
      money: money(leg.money, 'intent leg money'),
      fee: money(leg.fee, 'intent leg fee'),
    };
  });
  if (!Array.isArray(plan.signing_requirements)) throw new HumanDecodeError('intent signing requirements');
  const seen = new Set<number>();
  const signing: IntentSigningRequirement[] = plan.signing_requirements.map((entry) => {
    const requirement = record(entry, 'signing requirement');
    const legIndex = index(requirement.leg_index, 'signing requirement leg_index');
    if (legIndex >= legs.length || seen.has(legIndex)) throw new HumanDecodeError('signing requirement leg_index');
    seen.add(legIndex);
    return {
      leg_index: legIndex,
      action_key: hex32(requirement.action_key, 'signing requirement action_key'),
      signing_context: hex32(requirement.signing_context, 'signing requirement signing_context'),
      authority: text(requirement.authority, 'signing requirement authority'),
    };
  });
  const totalFee = money(plan.total_fee, 'intent total_fee');
  const summed = legs.reduce((sum, leg) => sum + BigInt(leg.fee.amount), 0n);
  if (summed !== BigInt(totalFee.amount)) throw new HumanDecodeError('intent total_fee does not equal the leg fees');
  return {
    plan_digest: hex32(plan.plan_digest, 'intent plan_digest'),
    journey_kind: text(plan.journey_kind, 'intent journey_kind'),
    total_fee: totalFee,
    legs,
    signing_requirements: signing,
  };
}

export function decodeIntentSubmission(value: unknown): IntentSubmission {
  const submission = record(value, 'intent submission');
  const journeyId = text(submission.journey_id, 'intent submission journey_id');
  if (!JOURNEY_ID.test(journeyId)) throw new HumanDecodeError('intent submission journey_id');
  return {
    journey_id: journeyId,
    plan_digest: hex32(submission.plan_digest, 'intent submission plan_digest'),
    state: oneOf(submission.state, JOURNEY_STATES, 'intent submission state'),
    state_copy_key: text(submission.state_copy_key, 'intent submission state_copy_key'),
  };
}

function evidence(value: unknown, what: string): EvidenceRef[] {
  if (!Array.isArray(value)) throw new HumanDecodeError(what);
  return value.map((entry) => {
    const ref = record(entry, `${what} entry`);
    const decoded: EvidenceRef = {
      evidence_id: text(ref.evidence_id, `${what} evidence_id`),
      class: oneOf(ref.class, EVIDENCE_CLASSES, `${what} class`),
      verification: oneOf(ref.verification, VERIFICATION_LEVELS, `${what} verification`),
    };
    if ('settlement_domain' in ref) decoded.settlement_domain = text(ref.settlement_domain, `${what} settlement_domain`);
    return decoded;
  });
}

export function decodeJourney(value: unknown): Journey {
  const journey = record(value, 'journey');
  if (!Array.isArray(journey.stages)) throw new HumanDecodeError('journey stages');
  const stages: JourneyStage[] = journey.stages.map((entry) => {
    const stage = record(entry, 'journey stage');
    return {
      stage_id: text(stage.stage_id, 'journey stage id'),
      copy_key: text(stage.copy_key, 'journey stage copy_key'),
      state: oneOf(stage.state, JOURNEY_STATES, 'journey stage state'),
      evidence: evidence(stage.evidence, 'journey stage evidence'),
    };
  });
  const decoded: Journey = {
    journey_id: text(journey.journey_id, 'journey id'),
    kind: oneOf(journey.kind, JOURNEY_KINDS, 'journey kind'),
    state: oneOf(journey.state, JOURNEY_STATES, 'journey state'),
    state_copy_key: text(journey.state_copy_key, 'journey state_copy_key'),
    stages,
    evidence: evidence(journey.evidence, 'journey evidence'),
    started_at: text(journey.started_at, 'journey started_at'),
    updated_at: text(journey.updated_at, 'journey updated_at'),
  };
  if ('refusal' in journey) decoded.refusal = record(journey.refusal, 'journey refusal');
  if ('wallet_request' in journey) decoded.wallet_request = record(journey.wallet_request, 'journey wallet_request');
  return decoded;
}
