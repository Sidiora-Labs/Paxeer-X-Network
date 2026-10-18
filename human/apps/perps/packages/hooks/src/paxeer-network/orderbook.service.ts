export interface RawOrderBook {
  asks: [number, number][];
  bids: [number, number][];
  ts: number;
}

export interface RawOrderBookUpdate {
  asks: [number, number][];
  bids: [number, number][];
  prevTs: number;
  ts: number;
}

export const defaultRawOrderBook: RawOrderBook = {
  asks: [],
  bids: [],
  ts: 0,
};

class OrderbookService {
  private static instance: OrderbookService;

  private bufferedOrderBookUpdates: {
    [symbol: string]: RawOrderBookUpdate[];
  } = {};

  private rawOrderBook: {
    [symbol: string]: RawOrderBook;
  } = {};

  constructor() {}

  static getInstance() {
    if (!this.instance) {
      this.instance = new OrderbookService();
    }
    return this.instance;
  }

  private sortBufferedOrderBookUpdates(symbol: string) {
    this.bufferedOrderBookUpdates[symbol]?.sort((a, b) => a.ts - b.ts);
  }

  private applyUpdateToRawOrderBook(
    symbol: string,
    update: RawOrderBookUpdate,
  ) {
    const rawOrderBook = this.rawOrderBook[symbol];

    if (!rawOrderBook || rawOrderBook.ts > update.prevTs) {
      return;
    }

    const askMap = new Map<number, number>();
    const bidMap = new Map<number, number>();

    rawOrderBook.asks.forEach((ask) => {
      askMap.set(ask[0], ask[1]);
    });

    rawOrderBook.bids.forEach((bid) => {
      bidMap.set(bid[0], bid[1]);
    });

    update.asks.forEach((ask) =>
      ask[1] === 0 ? askMap.delete(ask[0]) : askMap.set(ask[0], ask[1]),
    );
    update.bids.forEach((bid) =>
      bid[1] === 0 ? bidMap.delete(bid[0]) : bidMap.set(bid[0], bid[1]),
    );

    rawOrderBook.asks = Array.from<[number, number]>(askMap.entries()).sort(
      (a, b) => a[0] - b[0],
    );
    rawOrderBook.bids = Array.from<[number, number]>(bidMap.entries()).sort(
      (a, b) => b[0] - a[0],
    );

    rawOrderBook.ts = update.ts;
  }
  private applyBufferedUpdatesToRawOrderBooks(symbol: string) {
    this.bufferedOrderBookUpdates[symbol]?.forEach((update) => {
      this.applyUpdateToRawOrderBook(symbol, update);
    });
  }

  private deleteBufferedOrderBookUpdates(symbol: string) {
    delete this.bufferedOrderBookUpdates[symbol];
  }

  private commitOrderBook(symbol: string) {
    const rawOrderBook = this.rawOrderBook[symbol];
    if (!rawOrderBook) {
      return;
    }

    // const orderbook = this.prepareOrderBookStore(rawOrderBook);
    // if (orderbook.firstAskPrice <= orderbook.firstBidPrice) {
    //   console.error(SERVICE_PREFIX, "Orderbook crossing error", {
    //     crossedAsks: orderbook.asks,
    //     crossedBids: orderbook.bids,
    //   });
    //   this.rawOrderBook[symbol] = {
    //     ...defaultRawOrderBook,
    //     ts: -1, // must be -1 because it needs to cause Orderbook version error in public websocket
    //   };
    // } else {
    //   orderBookStore.update(symbol, orderbook);
    //   depthChartService.commitDepthChart(symbol);
    // }
  }

  private pushUpdateToBuffer(symbol: string, update: RawOrderBookUpdate) {
    if (this.bufferedOrderBookUpdates[symbol] === undefined) {
      this.bufferedOrderBookUpdates[symbol] = [];
    }

    const buffer = this.bufferedOrderBookUpdates[symbol];
    if (buffer.length > 0) {
      const lastUpdate = buffer[buffer.length - 1];
      if (lastUpdate.ts !== update.prevTs) {
        this.bufferedOrderBookUpdates[symbol] = [];
      }
    }

    this.bufferedOrderBookUpdates[symbol].push(update);
  }
  private isValidFullOrderBook(symbol: string, currentTs: number) {
    if ((this.bufferedOrderBookUpdates[symbol]?.length ?? 0) !== 0) {
      const earliestUpdates = this.bufferedOrderBookUpdates[symbol][0];
      // Incoming full orderbook is invalid if the timestamp is less than all of our buffered diff orderbook updates.
      return earliestUpdates.prevTs <= currentTs;
    }
    return true;
  }

  setFullOrderbook(symbol: string, rawOrderbook: RawOrderBook) {
    const { ts } = rawOrderbook;
    this.rawOrderBook[symbol] = rawOrderbook;
    this.sortBufferedOrderBookUpdates(symbol);
    if (this.isValidFullOrderBook(symbol, ts)) {
      this.applyBufferedUpdatesToRawOrderBooks(symbol);
    }
  }

  updateOrderbook(
    symbol: string,
    update: RawOrderBookUpdate,
    callback: () => void,
  ) {
    const { asks, bids, prevTs, ts } = update;
    const rawOrderBook = this.rawOrderBook[symbol];
    if (!rawOrderBook) {
      return;
    }
    const currentTs = rawOrderBook.ts;
    if (currentTs === 0) {
      this.pushUpdateToBuffer(symbol, { asks, bids, prevTs, ts });
      return;
    }
    if (prevTs !== currentTs) {
      this.pushUpdateToBuffer(symbol, { asks, bids, prevTs, ts });
      if (callback) {
        callback();
      }
      return;
    }
    this.applyUpdateToRawOrderBook(symbol, update);
    this.deleteBufferedOrderBookUpdates(symbol);
  }

  getRawOrderbook(symbol: string) {
    return this.rawOrderBook[symbol];
  }

  public resetOrderBook(symbol: string) {
    this.rawOrderBook[symbol] = defaultRawOrderBook;
  }
}

const orderBookService = OrderbookService.getInstance();

export default orderBookService;
