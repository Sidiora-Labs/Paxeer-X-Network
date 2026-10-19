import net from "node:net";
import {
  APPROVAL_DECISION_OUTCOMES,
  APPROVAL_EVENT_KINDS,
  Client,
  VerificationLevel,
  requireVerified,
} from "../../sdk/typescript/dist/src/index.js";

const [socketPath, scenarioList] = process.argv.slice(2);
if (!socketPath || !scenarioList) throw new Error("usage: typescript.mjs SOCKET SCENARIOS");

class ParityTransport {
  call(_operation, request) {
    return new Promise((resolve, reject) => {
      const socket = net.createConnection(socketPath);
      let response = "";
      socket.setEncoding("utf8");
      socket.on("connect", () => socket.write(`typescript\t${request.scenario}\n`));
      socket.on("data", (chunk) => { response += chunk; });
      socket.on("end", () => resolve(response.trim()));
      socket.on("error", reject);
    });
  }
}

function fields(encoded) {
  return Object.fromEntries(encoded.split(";").map((field) => field.split("=", 2)));
}

function validate(scenario, encoded) {
  const value = fields(encoded);
  if (scenario === "unknown_submission") {
    const state = { kind: "Unknown" };
    if (state.kind !== value.state) throw new Error("unknown submission collapsed");
  } else if (scenario === "terminal_rejection") {
    const state = { kind: "Failed", protocolResultCode: Number(value.result_code) };
    if (state.protocolResultCode !== -77777 || value.error !== "CoreRejection") {
      throw new Error("terminal result code changed");
    }
  } else if (scenario === "proven_read") {
    const read = {
      value: 1n,
      achievedVerificationLevel: VerificationLevel.StateProven,
      freshness: { chainHead: 10n, latestBatch: "22", latestCheckpoint: "genesis", valueSequence: 10n },
    };
    requireVerified(VerificationLevel.StateProven, read);
    if (value.verification !== "StateProven") throw new Error("proven read level changed");
  } else if (scenario === "availability_failure") {
    const error = { errorClass: value.error, protocolResultCode: null, retriable: false, requestId: 18n, reason: "capability_absent" };
    if (error.errorClass !== "UnavailableCapability") throw new Error("availability error changed");
  } else if (scenario === "subscription_gap" && value.state !== "Gap") {
    throw new Error("subscription gap hidden");
  } else if (scenario.startsWith("idempotency_") && (value.receipt_count !== "1" || value.economic_effects !== "1")) {
    throw new Error("idempotency duplicated an effect");
  } else if (scenario.startsWith("approval_event_")) {
    if (!APPROVAL_EVENT_KINDS.includes(value.state)) throw new Error("approval event vocabulary diverged");
  } else if (scenario.startsWith("approval_outcome_")) {
    if (!APPROVAL_DECISION_OUTCOMES.includes(value.state)) throw new Error("approval outcome vocabulary diverged");
  } else if (scenario.startsWith("approval_") && !["approval.list", "approval.get", "approval.approve", "approval.reject"].includes(value.state)) {
    throw new Error(`approval operation vocabulary diverged:${scenario}:${value.state}`);
  }
}

const client = new Client(new ParityTransport());
for (const scenario of scenarioList.split(",")) {
  const operation = ({
    approval_list: "approval.list",
    approval_get: "approval.get",
    approval_approve: "approval.approve",
    approval_reject: "approval.reject",
  })[scenario] ?? "track";
  const encoded = await client.call(operation, { scenario });
  validate(scenario, encoded);
  process.stdout.write(`${scenario}\t${encoded}\n`);
}
