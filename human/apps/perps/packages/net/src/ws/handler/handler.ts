import { MessageHandler } from "../../types/ws";
import PingHandler from "./ping";

export type MessageType =
  | "ping"
  | "pong"
  | "subscribe"
  | "unsubscribe"
  | "authenticate"
  | "message"
  | "error"
  | "auth"
  | "close";

export const messageHandlers = new Map<MessageType, MessageHandler>([
  ["ping", new PingHandler()],
]);
