import { MessageHandler } from "../../types/ws";

export default class BaseHandler implements MessageHandler {
  handle(message: any, webSocket: WebSocket) {
    throw new Error("Method not implemented.");
  }
}
