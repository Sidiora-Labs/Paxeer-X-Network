import { TpslCalService } from "./tpslCal.service";

jest.mock("@paxeer-network/i18n", () => ({
  i18n: { t: (key: string) => key },
}));

const order: any = {
  symbol: "PERP_ETH_USDC",
  margin_mode: "ISOLATED",
  algo_order_id: 11,
  root_algo_order_id: 10,
  parent_algo_type: "POSITIONAL_TP_SL",
  algo_type: "TAKE_PROFIT",
  type: "LIMIT",
  side: "SELL",
  quantity: 0,
  trigger_price: 4100,
  price: 4110,
  is_activated: true,
  is_triggered: false,
};

describe("TradingView Positional Limit estimates", () => {
  it("matches symbol and margin mode, and estimates Limit PnL using limit price", () => {
    const service = new TpslCalService();
    service.recalculatePnl(
      [
        { symbol: order.symbol, marginMode: "CROSS", balance: 2, open: 3900 },
        {
          symbol: order.symbol,
          marginMode: "ISOLATED",
          balance: 5,
          open: 4000,
        },
      ] as any,
      [order],
    );
    expect(service.getTpslQuantity(order)).toBe(5);
    expect(service.getFormattedEstPnl(order).toString()).toBe("550");
    expect(
      service
        .getFormattedEstPnl({ ...order, type: "CLOSE_POSITION" })
        .toString(),
    ).toBe("500");
  });
  it("does not change execution quantity when the position changes or disappears", () => {
    const service = new TpslCalService();
    const triggered = {
      ...order,
      is_triggered: true,
      quantity: 3,
      total_executed_quantity: 1,
    };
    service.recalculatePnl(
      [
        {
          symbol: order.symbol,
          marginMode: "ISOLATED",
          balance: 9,
          open: 4000,
        },
      ] as any,
      [triggered],
    );
    expect(service.getTpslQuantity(triggered)).toBe(3);
    service.recalculatePnl(
      [
        { symbol: order.symbol, marginMode: "CROSS", balance: 100, open: 4000 },
      ] as any,
      [triggered],
    );
    expect(service.getTpslQuantity(triggered)).toBe(3);
    expect(service.getFormattedEstPnl(triggered)).toBe("");
    expect(service.getTpslQuantity(order)).toBeUndefined();
  });
});
