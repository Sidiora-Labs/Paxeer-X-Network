import { createHash, randomUUID } from "node:crypto";
import { closeSync, constants, existsSync, fstatSync, lstatSync, mkdirSync, openSync, readFileSync, realpathSync, writeSync } from "node:fs";
import { dirname, isAbsolute, resolve } from "node:path";
import { DatabaseSync } from "node:sqlite";
import { createInterface } from "node:readline/promises";
import { ReadStream, WriteStream, isatty } from "node:tty";
import { PaxeerWallet } from "../../../human/wallet/sdk/dist/index.js";

const required = (env, name) => {
  const value = env[name];
  if (typeof value !== "string" || !value || value.includes("\0")) throw new Error(`missing_${name.toLowerCase()}`);
  return value;
};
const digest = (value) => createHash("sha256").update(value).digest("hex");
const identifier = (value) => {
  if (typeof value !== "string" || !/^[0-9a-f]{64}$/u.test(value)) throw new Error("invalid_preparation_reference");
  return value;
};
function protectedPath(path, directory = false) {
  if (!isAbsolute(path) || resolve(path) !== path || realpathSync(path) !== path) throw new Error("noncanonical_owner_path");
  const stat = lstatSync(path);
  if (stat.uid !== process.getuid() || (stat.mode & 0o077) !== 0 || stat.isSymbolicLink()
    || (directory ? !stat.isDirectory() : !stat.isFile())) throw new Error("unprotected_owner_path");
}
function protectedJson(path) {
  protectedPath(path);
  const fd = openSync(path, constants.O_RDONLY | constants.O_NOFOLLOW);
  try {
    const stat = fstatSync(fd);
    if (!stat.isFile() || stat.uid !== process.getuid() || (stat.mode & 0o077) !== 0 || stat.size > 1048576) throw new Error("invalid_owner_session_file");
    return JSON.parse(readFileSync(fd, "utf8"));
  } finally { closeSync(fd); }
}
function processStart(pid) {
  try { return readFileSync(`/proc/${pid}/stat`, "utf8").split(") ").at(-1).split(" ")[19]; }
  catch (error) { if (error.code === "ENOENT") return null; throw error; }
}
const schemas = {
  context: "CREATE TABLE context (singleton INTEGER PRIMARY KEY CHECK(singleton=1), binding TEXT NOT NULL) STRICT",
  lease: "CREATE TABLE lease (singleton INTEGER PRIMARY KEY CHECK(singleton=1), pid INTEGER NOT NULL, started TEXT NOT NULL, token TEXT NOT NULL) STRICT",
  records: "CREATE TABLE records (key TEXT PRIMARY KEY, value TEXT NOT NULL) STRICT",
};
class OwnerStore {
  constructor(path, binding) {
    if (process.platform !== "linux") throw new Error("owner_store_requires_linux_process_identity");
    this.path = path; this.token = randomUUID(); this.closed = false;
    mkdirSync(dirname(path), { recursive: true, mode: 0o700 });
    protectedPath(dirname(path), true);
    if (!existsSync(path)) {
      try { closeSync(openSync(path, "wx", 0o600)); } catch (error) { if (error.code !== "EEXIST") throw error; }
    }
    protectedPath(path);
    this.files();
    this.db = new DatabaseSync(path, { timeout: 5000, allowExtension: false, enableDoubleQuotedStringLiterals: false });
    try {
      this.db.exec("PRAGMA trusted_schema=OFF; PRAGMA synchronous=FULL; BEGIN IMMEDIATE");
      const objects = this.db.prepare("SELECT name, sql FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").all();
      if (objects.length === 0) {
        if (this.db.prepare("PRAGMA application_id").get().application_id !== 0 || this.db.prepare("PRAGMA user_version").get().user_version !== 0) throw new Error("owner_store_schema_conflict");
        this.db.exec(`${Object.values(schemas).join(";")}; PRAGMA application_id=1280857935; PRAGMA user_version=1`);
        this.db.prepare("INSERT INTO context VALUES(1, ?)").run(JSON.stringify(binding));
      } else if (objects.length !== 3 || objects.some((row) => schemas[row.name] !== row.sql)
        || this.db.prepare("PRAGMA application_id").get().application_id !== 1280857935
        || this.db.prepare("PRAGMA user_version").get().user_version !== 1) throw new Error("owner_store_schema_conflict");
      if (this.db.prepare("SELECT binding FROM context WHERE singleton=1").get()?.binding !== JSON.stringify(binding)) throw new Error("owner_store_identity_conflict");
      const lease = this.db.prepare("SELECT * FROM lease WHERE singleton=1").get();
      if (lease && (!Number.isSafeInteger(lease.pid) || lease.pid <= 0 || !/^[0-9]+$/u.test(lease.started) || !/^[0-9a-f-]{36}$/u.test(lease.token))) throw new Error("owner_store_lease_corrupt");
      if (lease && processStart(lease.pid) === lease.started) throw new Error("owner_store_in_use");
      const started = processStart(process.pid);
      if (!started) throw new Error("owner_process_identity_unavailable");
      this.db.prepare("INSERT OR REPLACE INTO lease VALUES(1, ?, ?, ?)").run(process.pid, started, this.token);
      this.db.exec("COMMIT; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA max_page_count=16384");
      const check = this.db.prepare("PRAGMA quick_check").all();
      if (check.length !== 1 || check[0].quick_check !== "ok") throw new Error("owner_store_corrupt");
      this.files();
    } catch (error) { this.db.close(); throw error; }
  }
  files() { for (const path of [this.path, `${this.path}-wal`, `${this.path}-shm`]) if (existsSync(path)) protectedPath(path); }
  getItem(key) {
    this.assertLease();
    return this.db.prepare("SELECT value FROM records WHERE key=?").get(key)?.value ?? null;
  }
  setItem(key, value) {
    if (typeof key !== "string" || key.length > 1024 || typeof value !== "string" || Buffer.byteLength(value) > 1048576) throw new Error("owner_store_bounds");
    this.db.exec("BEGIN IMMEDIATE");
    try {
      this.assertLease();
      if (this.db.prepare("SELECT count(*) AS n FROM records").get().n >= 4096 && this.getItem(key) === null) throw new Error("owner_store_full");
      this.db.prepare("INSERT INTO records VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value=excluded.value").run(key, value);
      this.db.exec("COMMIT"); this.files();
    } catch (error) { try { this.db.exec("ROLLBACK"); } catch {} throw error; }
  }
  assertLease() {
    if (this.closed || this.db.prepare("SELECT token FROM lease WHERE singleton=1").get()?.token !== this.token) throw new Error("owner_store_lease_lost");
  }
  close() {
    if (!this.closed) {
      try { this.db.prepare("DELETE FROM lease WHERE singleton=1 AND token=?").run(this.token); }
      finally { this.closed = true; this.db.close(); }
    }
  }
}
async function confirmAtTerminal(review) {
  let fd;
  try { fd = openSync("/dev/tty", "r+"); } catch { throw new Error("explicit_owner_terminal_required"); }
  if (!isatty(fd)) { closeSync(fd); throw new Error("explicit_owner_terminal_required"); }
  let outputFd;
  try { outputFd = openSync("/dev/tty", "w"); } catch (error) { closeSync(fd); throw error; }
  const input = new ReadStream(fd);
  const output = new WriteStream(outputFd);
  const rl = createInterface({ input, output, terminal: true });
  const answer = `APPROVE ${review.id} ${digest(Buffer.from(review.activity, "hex"))}`;
  try {
    writeSync(outputFd, `Original wallet review (no signing occurs during this approval):\n${JSON.stringify(review, null, 2)}\n`);
    return await rl.question(`Type exactly ${answer}\n> `, { signal: AbortSignal.timeout(300000) }) === answer;
  } finally { rl.close(); input.destroy(); output.destroy(); }
}
let active;
export function ownerWalletHandoff() {
  if (!active) throw new Error("original_wallet_services_not_open");
  return active.handoff;
}
export async function createAgentServices(environment) {
  if (active) throw new Error("original_wallet_services_already_open");
  const major = Number(process.versions.node.split(".")[0]);
  const minor = Number(process.versions.node.split(".")[1]);
  if (major < 22 || major === 22 && minor < 18) throw new Error("node_22_18_required");
  const principal = required(environment, "LAYERX_WALLET_PRINCIPAL");
  const apiUrl = required(environment, "LAYERX_WALLET_API_URL");
  const supabaseUrl = required(environment, "LAYERX_WALLET_SUPABASE_URL");
  for (const raw of [apiUrl, supabaseUrl]) {
    const url = new URL(raw);
    if (url.protocol !== "https:" || url.username || url.password || url.search || url.hash) throw new Error("wallet_https_endpoint_required");
  }
  const receiptPath = required(environment, "LAYERX_DAEMON_RECEIPT_STORE_PATH");
  const preparationPath = required(environment, "LAYERX_DAEMON_PREPARATION_STORE_PATH");
  const storePath = required(environment, "LAYERX_WALLET_STORE_PATH");
  for (const path of [receiptPath, preparationPath, storePath]) if (!isAbsolute(path) || resolve(path) !== path) throw new Error("absolute_store_path_required");
  if (new Set([receiptPath, preparationPath, storePath]).size !== 3) throw new Error("distinct_stores_required");
  const store = new OwnerStore(storePath, { principal, apiUrl, supabaseUrl,
    tenant: required(environment, "LAYERX_TENANT"), actor: required(environment, "LAYERX_ACTOR"),
    sessionId: required(environment, "LAYERX_SESSION_ID"), receiptPath, preparationPath });
  let wallet, subscription;
  try {
    wallet = new PaxeerWallet({ apiUrl, supabaseUrl,
      supabaseAnonKey: required(environment, "LAYERX_WALLET_SUPABASE_ANON_KEY"),
      lxActivity: { storage: store, confirm: confirmAtTerminal } });
    const saved = store.getItem("owner:session");
    const session = saved === null ? protectedJson(required(environment, "LAYERX_WALLET_SESSION_FILE")) : JSON.parse(saved);
    if (typeof session.access_token !== "string" || typeof session.refresh_token !== "string") throw new Error("original_session_required");
    const { data, error } = await wallet.supabase.auth.setSession({ access_token: session.access_token, refresh_token: session.refresh_token });
    if (error || !data.session || (await wallet.getUser())?.id !== principal) throw new Error("original_wallet_principal_mismatch");
    store.setItem("owner:session", JSON.stringify(data.session));
    subscription = wallet.onAuthStateChange((_event, next) => {
      if (next?.user.id === principal) store.setItem("owner:session", JSON.stringify(next));
    });
    const handoff = {
      async review(prepared) {
        const id = identifier(prepared.preparation_ref);
        if (typeof prepared.unsigned_canonical_bytes !== "string" || !/^(?:[0-9a-f]{2})+$/u.test(prepared.unsigned_canonical_bytes)
          || digest(Buffer.from(prepared.unsigned_canonical_bytes, "hex")) !== id) throw new Error("prepared_bytes_mismatch");
        const key = `owner:preparation:${id}`;
        const old = store.getItem(key);
        if (old !== null) {
          const retained = JSON.parse(old);
          if (JSON.stringify(retained.prepared) !== JSON.stringify(prepared)) throw new Error("prepared_handoff_conflict");
          if (!retained.reviewId) throw new Error("wallet_review_outcome_unknown");
          return wallet.lxApprovalStatus(retained.reviewId);
        }
        store.setItem(key, JSON.stringify({ prepared, reviewId: null }));
        const review = await wallet.reviewLxActivity(prepared.unsigned_canonical_bytes);
        if (review.kind !== "lx_activity" || review.activity !== prepared.unsigned_canonical_bytes
          || review.signing_preimage !== prepared.signing_preimage || review.public_key !== prepared.signer_public_key) throw new Error("wallet_preparation_binding_failed");
        store.setItem(key, JSON.stringify({ prepared, reviewId: review.id }));
        return review;
      },
      async approve(preparationRef, reviewId) {
        const retained = JSON.parse(store.getItem(`owner:preparation:${identifier(preparationRef)}`) ?? "null");
        if (!retained || retained.reviewId !== reviewId) throw new Error("original_review_handoff_required");
        return wallet.approveLxActivity(reviewId);
      },
      async status(preparationRef, reviewId) {
        const retained = JSON.parse(store.getItem(`owner:preparation:${identifier(preparationRef)}`) ?? "null");
        if (!retained || retained.reviewId !== reviewId) throw new Error("original_review_handoff_required");
        return wallet.lxApprovalStatus(reviewId);
      },
    };
    active = { handoff };
    return { walletSigner: { client: wallet }, daemonReceiptStorePath: receiptPath,
      daemonPreparationStorePath: preparationPath,
      async destroy() {
        if (active?.handoff === handoff) active = undefined;
        subscription?.(); await wallet.supabase.auth.stopAutoRefresh(); store.close();
      } };
  } catch (error) {
    subscription?.(); if (wallet) await wallet.supabase.auth.stopAutoRefresh(); store.close(); throw error;
  }
}
