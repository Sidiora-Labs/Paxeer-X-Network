export type MessageObserveTopic = {
  event: "subscribe" | "unsubscribe";
  topic: string;
};

export type MessageObserveParams = string | MessageObserveTopic;

export type SendFunc = (message: any) => void;

export interface MessageHandler {
  // wsSubject: WebSocketSubject<any>;
  handle: (message: any, webSocket: WebSocket) => void;
}
