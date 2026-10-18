import mockReact from "react";
import { act, create } from "react-test-renderer";
import {
  AlgoOrderRootType,
  MarginMode,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { useTPSLDetail } from "./tpslDetail.script";

let mockMainOrders: any[] = [];
let mockIsMobile = false;
const mockModalShow = jest.fn();

const mockOrderActions = {
  cancelAlgoOrder: jest.fn(),
  cancelPostionOrdersByTypes: jest.fn(),
};

jest.mock("@paxeer-network/hooks", () => ({
  findPositionTPSLFromOrders: (orders: any[]) => ({
    fullPositionOrder: orders.find(
      (order) => order.algo_type === "POSITIONAL_TP_SL",
    ),
    partialPositionOrders: orders.filter(
      (order) => order.algo_type === "TP_SL",
    ),
  }),
  useAccount: () => ({ state: { mainAccountId: "main-account" } }),
  useEventEmitter: () => ({ emit: jest.fn() }),
  useOrderStream: () => [mockMainOrders, mockOrderActions],
  useSubAccountAlgoOrderStream: () => [
    [],
    {
      cancelAlgoOrder: jest.fn(),
      cancelPostionOrdersByTypes: jest.fn(),
      refresh: jest.fn(),
    },
  ],
  useSymbolsInfo: () => ({ PERP_ETH_USDC: jest.fn() }),
}));

jest.mock("@paxeer-network/ui", () => ({
  modal: { show: (...args: any[]) => mockModalShow(...args) },
  useScreen: () => ({ isMobile: mockIsMobile }),
}));

jest.mock("../positionTPSL/tpsl.widget", () => ({
  TPSLDialogId: "TPSLDialogId",
  TPSLSheetId: "TPSLSheetId",
}));

const position = {
  symbol: "PERP_ETH_USDC",
  position_qty: 2,
  average_open_price: 4000,
  margin_mode: MarginMode.CROSS,
} as any;

const createOrder = (triggerPrice: number) =>
  ({
    algo_order_id: 1,
    algo_type: AlgoOrderRootType.TP_SL,
    symbol: position.symbol,
    quantity: 2,
    type: OrderType.MARKET,
    child_orders: [
      {
        algo_order_id: 2,
        algo_type: "TAKE_PROFIT",
        type: OrderType.MARKET,
        trigger_price: triggerPrice,
      },
    ],
  }) as any;

describe("useTPSLDetail controlled editor", () => {
  beforeEach(() => {
    mockIsMobile = false;
    mockMainOrders = [createOrder(4200)];
    jest.clearAllMocks();
  });

  it("tracks the latest order by id and closes when it disappears", () => {
    let state: ReturnType<typeof useTPSLDetail>;
    const Harness = () => {
      state = useTPSLDetail({
        position,
        order: mockMainOrders[0],
        baseDP: 4,
        quoteDP: 2,
      });
      return null;
    };

    let tree: ReturnType<typeof create>;
    act(() => {
      tree = create(mockReact.createElement(Harness));
    });

    act(() => {
      state!.editTPSLOrder(mockMainOrders[0], PositionType.PARTIAL);
    });
    expect(state!.tpslEditOpen).toBe(true);
    expect(state!.tpslEditMode).toBe("dialog");
    expect(state!.tpslEditOrder?.child_orders[0].trigger_price).toBe(4200);

    mockMainOrders = [createOrder(4300)];
    act(() => {
      tree!.update(mockReact.createElement(Harness));
    });
    expect(state!.tpslEditOrder).toBe(mockMainOrders[0]);
    expect(state!.tpslEditOrder?.child_orders[0].trigger_price).toBe(4300);

    mockMainOrders = [];
    act(() => {
      tree!.update(mockReact.createElement(Harness));
    });
    expect(state!.tpslEditOpen).toBe(false);
    expect(state!.tpslEditOrder).toBeUndefined();
    tree!.unmount();
  });

  it("uses the controlled sheet for mobile edits and keeps create on modal.show", () => {
    mockIsMobile = true;
    let state: ReturnType<typeof useTPSLDetail>;
    const Harness = () => {
      state = useTPSLDetail({
        position,
        order: mockMainOrders[0],
        baseDP: 4,
        quoteDP: 2,
      });
      return null;
    };

    let tree: ReturnType<typeof create>;
    act(() => {
      tree = create(mockReact.createElement(Harness));
    });

    act(() => {
      state!.editTPSLOrder(mockMainOrders[0], PositionType.PARTIAL);
    });
    expect(state!.tpslEditOpen).toBe(true);
    expect(state!.tpslEditMode).toBe("sheet");
    expect(state!.tpslEditOrder).toBe(mockMainOrders[0]);
    expect(mockModalShow).not.toHaveBeenCalled();

    act(() => {
      state!.onTPSLEditOpenChange(false);
      state!.addTPSLOrder(PositionType.PARTIAL);
    });
    expect(mockModalShow).toHaveBeenCalledWith("TPSLSheetId", {
      symbol: position.symbol,
      position,
      positionType: PositionType.PARTIAL,
      isEditing: false,
    });
    tree!.unmount();
  });
});
