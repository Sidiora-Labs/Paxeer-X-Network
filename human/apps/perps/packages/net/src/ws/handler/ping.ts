import BaseHandler from "./baseHandler";

export default class PingHandler extends BaseHandler {
  handle(_: any, webSocket: WebSocket) {
    webSocket.send(JSON.stringify({ event: "pong", ts: Date.now() }));
  }
}
