const { test } = require("node:test");
const assert = require("node:assert/strict");
const http = require("node:http");
const { startCallbackServer } = require("../src/internal/login-server");

const STATE = "test-state";

async function start(t, state = STATE) {
  const handle = startCallbackServer({ port: 0, state });
  // Tests that end without a handoff are cleaned up via cancel(), which
  // rejects waitForToken; mark that expected rejection as handled.
  handle.waitForToken.catch(() => {});
  const port = await handle.listen();
  t.after(() => handle.cancel());
  return { ...handle, port };
}

function request(port, path, { method = "GET", agent } = {}) {
  return new Promise((resolve, reject) => {
    const req = http.request(
      { host: "127.0.0.1", port, path, method, agent },
      (res) => {
        let body = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => {
          body += chunk;
        });
        res.on("end", () => {
          resolve({
            status: res.statusCode,
            contentType: res.headers["content-type"],
            body,
          });
        });
      },
    );
    req.on("error", reject);
    req.end();
  });
}

const successPath =
  "/callback?access_token=access&refresh_token=refresh" +
  `&state=${STATE}&username=0xabc&avatar_url=`;

test("listens on the loopback interface only", async (t) => {
  const { server } = await start(t);
  assert.equal(server.address().address, "127.0.0.1");
});

test("resolves the token pair and serves the success page", async (t) => {
  const { port, waitForToken } = await start(t);
  const res = await request(port, successPath);
  assert.equal(res.status, 200);
  assert.match(res.contentType, /^text\/html/);
  assert.match(res.body, /Login Successful/);
  assert.deepEqual(await waitForToken, {
    token: "access",
    refreshToken: "refresh",
    state: STATE,
    username: "0xabc",
    avatarUrl: "",
  });
});

test("stops accepting new connections after the handoff", async (t) => {
  const { port } = await start(t);
  const agent = new http.Agent({ keepAlive: false });
  await request(port, successPath, { agent });
  await assert.rejects(request(port, successPath, { agent }), /ECONNREFUSED/);
});

test("answers a duplicate callback on a kept-alive connection idempotently", async (t) => {
  const { port, waitForToken } = await start(t);
  const agent = new http.Agent({ keepAlive: true });
  t.after(() => agent.destroy());
  const first = await request(port, successPath, { agent });
  const second = await request(port, successPath, { agent });
  assert.equal(first.status, 200);
  assert.equal(second.status, 200);
  assert.match(second.body, /Login Successful/);
  assert.equal((await waitForToken).token, "access");
});

test("rejects a state mismatch with the failure page", async (t) => {
  const { port } = await start(t);
  const res = await request(
    port,
    "/callback?access_token=access&refresh_token=refresh&state=wrong",
  );
  assert.equal(res.status, 400);
  assert.match(res.body, /Login Failed/);
  assert.match(res.body, /Invalid state token/);
});

test("rejects a missing token", async (t) => {
  const { port } = await start(t);
  const res = await request(
    port,
    `/callback?refresh_token=refresh&state=${STATE}`,
  );
  assert.equal(res.status, 400);
  assert.match(res.body, /Missing token/);
});

test("rejects a missing refresh token", async (t) => {
  const { port } = await start(t);
  const res = await request(
    port,
    `/callback?access_token=access&state=${STATE}`,
  );
  assert.equal(res.status, 400);
  assert.match(res.body, /Missing refresh token/);
});

test("accepts the legacy token query parameter", async (t) => {
  const { port, waitForToken } = await start(t);
  const res = await request(
    port,
    `/callback?token=legacy&refresh_token=refresh&state=${STATE}`,
  );
  assert.equal(res.status, 200);
  assert.equal((await waitForToken).token, "legacy");
});

test("returns 404 outside /callback", async (t) => {
  const { port } = await start(t);
  const res = await request(port, "/other");
  assert.equal(res.status, 404);
  assert.equal(res.body, "Not Found");
});

test("returns 405 for non-GET methods", async (t) => {
  const { port } = await start(t);
  const res = await request(port, successPath, { method: "POST" });
  assert.equal(res.status, 405);
  assert.equal(res.body, "Method Not Allowed");
});

test("cancel rejects the pending handoff and closes the server", async () => {
  const handle = startCallbackServer({ port: 0, state: STATE });
  const port = await handle.listen();
  handle.cancel(new Error("Login timed out"));
  await assert.rejects(handle.waitForToken, /Login timed out/);
  await assert.rejects(request(port, successPath), /ECONNREFUSED/);
});
