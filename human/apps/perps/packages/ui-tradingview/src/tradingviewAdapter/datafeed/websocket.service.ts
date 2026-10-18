import { mapResolution } from "../../utils/common.util";
import { WS } from "@paxeer-network/net";

const getKlineKey = (symbol: string, type: string) => `${symbol}kline_${type}`;

const getSymbolTopics = (symbol: string) => {
  const symbolTopics = ["trade"];
  return symbolTopics.map((topic) => `${symbol}@${topic}`);
};

export class WebsocketService {
  static _created = false;
  static _instance: any = null;
  private klineSubscribeIdMap: Map<string, any> = new Map();
  private klineOnTickCallback = new Map();
  subscribeCachedTopics = new Map<string, any>();
  private wsInstance: WS | null = null;

  private klineData = new Map();

  constructor(ws: WS) {
    if (!WebsocketService._created) {
      this.wsInstance = ws;
      WebsocketService._instance = this;
      WebsocketService._created = true;
    }

    return WebsocketService._instance;
  }

  subscribeKline(
    subscribeId: string,
    symbol: any,
    resolution: any,
    onTickCallback: any
  ) {
    const time = mapResolution(resolution);

    this.klineSubscribeIdMap.set(subscribeId, { symbol, resolution });

    const klineKey = getKlineKey(symbol, time);
    if (this.klineOnTickCallback.has(klineKey)) {
      const value = this.klineOnTickCallback.get(klineKey);
      value[subscribeId] = onTickCallback;
    } else {
      this.klineOnTickCallback.set(klineKey, {
        [subscribeId]: onTickCallback,
      });
      const unsub = this.wsInstance?.subscribe(
        {
          event: "subscribe",
          topic: `${symbol}@kline_${time}`,
          id: `${symbol}@kline_${time}`,
          ts: new Date().getTime(),
        },
        {
          onMessage: (data) => {
            const { open, close, high, low, volume, startTime } = data;
            const key = getKlineKey(data.symbol, data.type);
            this.updateKline(key, {
              time: startTime,
              close,
              open,
              high,
              low,
              volume,
            });
          },
        }
      );
      this.subscribeCachedTopics.set(`${symbol}@kline_${time}`, unsub);
    }
  }

  unsubscribeKline(subscribeId: string) {
    if (!this.klineSubscribeIdMap.has(subscribeId)) {
      return;
    }

    const { symbol, resolution } = this.klineSubscribeIdMap.get(subscribeId);
    const time = mapResolution(resolution);
    const klineKey = getKlineKey(symbol, time);
    if (this.klineOnTickCallback.has(klineKey)) {
      const value = this.klineOnTickCallback.get(klineKey);
      delete value[subscribeId];

      if (Object.keys(value).length === 0) {
        this.klineOnTickCallback.delete(klineKey);
        const unsub = this.subscribeCachedTopics.get(`${symbol}@kline_${time}`);
        unsub();
      }
    }

    // @ts-ignore
    delete this.klineSubscribeIdMap[subscribeId];
  }

  subscribeSymbol(symbol: string) {
    const symbolTopics = getSymbolTopics(symbol);
    symbolTopics.forEach((topic) => {
      // check if subscribed
      if (!this.subscribeCachedTopics.has(topic)) {
        const unsub = this.wsInstance?.subscribe(
          {
            event: "subscribe",
            topic: topic,
            id: topic,
            ts: new Date().getTime(),
          },
          {
            onMessage: (data) => {
              this.updateKlineByLastPrice(data.symbol, data.price);
            },
          }
        );
        this.subscribeCachedTopics.set(topic, unsub);
      }
    });
  }

  updateKlineByLastPrice(symbol: string, lastPrice: number) {
    this.klineOnTickCallback.forEach((_, key) => {
      if (key.startsWith(symbol)) {
        const klineData = this.klineData.get(key);
        if (klineData) {
          this.updateKline(key, { ...klineData, close: lastPrice });
        }
      }
    });
  }

  updateKline(key: string, cbParams: any) {
    const onTickCbs = this.klineOnTickCallback.get(key);
    if (onTickCbs && cbParams) {
      this.klineData.set(key, cbParams);

      Object.keys(onTickCbs).forEach((key: any) => {
        const onTickCb = onTickCbs[key];
        if (onTickCb && typeof onTickCb === "function") {
          onTickCb(cbParams);
        }
      });
    }
  }
}
