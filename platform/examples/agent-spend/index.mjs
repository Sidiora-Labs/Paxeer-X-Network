import { AgentMiddleware } from "@sidiora/layerx-agent-middleware";
import { ProductionClient, isSelectableProtocolVersion } from "@sidiora/layerx-sdk";
import { LayerXAgentTransport, authenticatedAgentTransport, loadAgentServiceProviders } from "@sidiora/layerx-agent-integrations";

const required = (name) => {
  const value = process.env[name];
  if (value === undefined || value.length === 0) throw new Error(`missing_${name.toLowerCase()}`);
  return value;
};
const protocolVersion = Number(required("LAYERX_PROTOCOL_VERSION"));
if (!isSelectableProtocolVersion(protocolVersion)) throw new Error("invalid_layerx_protocol_version");
const providers = await loadAgentServiceProviders(process.env);
const authenticated = authenticatedAgentTransport(process.env, {
  agentRpcUrl: required("LAYERX_AGENT_RPC_URL"),
  tenant: required("LAYERX_TENANT"),
  requestTimeoutMs: 30000,
});
try {
  const request = JSON.parse(required("LAYERX_SPEND_REQUEST_JSON"));
  if (request === null || typeof request !== "object" || Array.isArray(request)
    || request.tenant !== required("LAYERX_TENANT")) throw new Error("invalid_spend_request");
  const middleware = new AgentMiddleware({
    client: new ProductionClient(new LayerXAgentTransport(authenticated.transport)),
    protocolVersion,
    ...(providers.budgets === undefined ? {} : { budgets: providers.budgets }),
    ...(providers.preparationBudgets === undefined ? {} : { preparationBudgets: providers.preparationBudgets }),
    signer: providers.signer,
    receipts: providers.receipts,
  });
  const result = await middleware.spend(request);
  process.stdout.write(JSON.stringify({
    kind: result.kind,
    ...(result.kind === "owner-budget" ? { preparationId: result.preparationId, admissionObserved: result.admissionObserved, ownerState: result.state,
      ...(result.verification === undefined ? {} : { receiptDigest: Buffer.from(result.verification.receiptDigest).toString("hex") }) } : {}),
    ...(result.kind === "verified" ? { receiptDigest: Buffer.from(result.verification.receiptDigest).toString("hex") } : {}),
    ...(result.kind === "approval-hold" ? { approvalId: result.approval.approvalId } : {}),
    ...(result.kind === "refused" || result.kind === "budget-refused" ? { code: result.code, retry: result.retry,
      ...(result.retryAfterMs === undefined ? {} : { retryAfterMs: result.retryAfterMs }) } : {}),
    ...("reservation" in result ? { reservationState: result.reservation.state } : {}),
  }) + "\n");
  if (!(result.kind === "owner-budget" ? ["settled", "approval", "pending"].includes(result.state) : ["verified", "approval-hold", "pending"].includes(result.kind))) process.exitCode = 2;
} finally {
  authenticated.destroy();
  await providers.destroy?.();
}
