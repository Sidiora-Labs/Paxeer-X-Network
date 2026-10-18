const http = require("http");

const SUCCESS_HTML = `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Login Successful</title></head>
<body style="display:flex;justify-content:center;align-items:center;height:100vh;margin:0;font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0">
  <div style="text-align:center">
    <div style="font-size:48px;margin-bottom:16px">&#10003;</div>
    <h1 style="margin:0 0 8px">Login Successful!</h1>
    <p style="color:#94a3b8">You can close this tab and return to the CLI.</p>
  </div>
</body>
</html>`;

const ERROR_HTML = (msg) => `<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>Login Failed</title></head>
<body style="display:flex;justify-content:center;align-items:center;height:100vh;margin:0;font-family:system-ui,sans-serif;background:#0f172a;color:#e2e8f0">
  <div style="text-align:center">
    <div style="font-size:48px;margin-bottom:16px">&#10007;</div>
    <h1 style="margin:0 0 8px">Login Failed</h1>
    <p style="color:#f87171">${msg}</p>
  </div>
</body>
</html>`;

function startCallbackServer({ port, state }) {
  let settled = false;
  let resolveToken;
  let rejectToken;

  const waitForToken = new Promise((resolve, reject) => {
    resolveToken = resolve;
    rejectToken = reject;
  });

  /**
   * Reject the login wait (e.g. timeout) and close the server once.
   * @param {Error | string} reason
   */
  function cancel(reason) {
    if (settled) {
      return;
    }
    settled = true;
    const error =
      reason instanceof Error
        ? reason
        : new Error(String(reason || "Cancelled"));
    rejectToken(error);
    server.close();
  }

  const server = http.createServer((req, res) => {
    const url = new URL(req.url, `http://localhost:${port}`);

    // Only handle /callback
    if (url.pathname !== "/callback") {
      res.writeHead(404, { "Content-Type": "text/plain" });
      res.end("Not Found");
      return;
    }

    // Only accept GET
    if (req.method !== "GET") {
      res.writeHead(405, { "Content-Type": "text/plain" });
      res.end("Method Not Allowed");
      return;
    }

    // Read token and state from URL query parameters
    // Support both "token" and "access_token" for compatibility
    const token =
      url.searchParams.get("token") || url.searchParams.get("access_token");
    const refreshToken = url.searchParams.get("refresh_token");
    const receivedState = url.searchParams.get("state");
    const username = url.searchParams.get("username");
    const avatarUrl = url.searchParams.get("avatar_url");

    // Validate CSRF state
    if (!receivedState || receivedState !== state) {
      res.writeHead(400, { "Content-Type": "text/html; charset=utf-8" });
      res.end(ERROR_HTML("Invalid state token. Please try again."));
      return;
    }

    // Validate token
    if (!token || typeof token !== "string") {
      res.writeHead(400, { "Content-Type": "text/html; charset=utf-8" });
      res.end(ERROR_HTML("Missing token. Please try again."));
      return;
    }
    if (!refreshToken || typeof refreshToken !== "string") {
      res.writeHead(400, { "Content-Type": "text/html; charset=utf-8" });
      res.end(ERROR_HTML("Missing refresh token. Please try again."));
      return;
    }

    if (settled) {
      res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
      res.end(SUCCESS_HTML);
      return;
    }

    settled = true;

    // Success — respond first, then resolve
    res.writeHead(200, { "Content-Type": "text/html; charset=utf-8" });
    res.end(SUCCESS_HTML);

    resolveToken({
      token,
      refreshToken,
      state: receivedState,
      username,
      avatarUrl: avatarUrl,
    });
    server.close();
  });

  /**
   * Listen on the loopback interface only. The browser delivers the OAuth
   * callback via top-level navigation to http://localhost:<port>, so no other
   * network interface needs to reach this server.
   * @returns {Promise<number>} resolves with the bound port
   */
  function listen() {
    return new Promise((resolve, reject) => {
      server.once("error", reject);
      server.listen(port, "127.0.0.1", () => {
        resolve(server.address().port);
      });
    });
  }

  return { server, waitForToken, cancel, listen };
}

module.exports = { startCallbackServer };
