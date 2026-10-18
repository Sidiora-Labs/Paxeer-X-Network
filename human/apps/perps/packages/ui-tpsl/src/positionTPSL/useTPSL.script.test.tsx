import React from "react";
import { act, create, ReactTestRenderer } from "react-test-renderer";
import {
  AlgoOrderRootType,
  MarginMode,
  OrderSide,
  OrderType,
  PositionType,
} from "@paxeer-network/types";
import { TPSLBuilderOptions, useTPSLBuilder } from "./useTPSL.script";

let mockMarginMode: MarginMode | undefined;
let mockPositions: any[] = [];
let mockSlPriceError: any = null;
const mockClose = jest.fn();
const mockSetValue = jest.fn();
const mockSetValues = jest.fn();
const mockIsTPSLOrderTypeLocked = jest.fn(
  (_order?: unknown, _leg?: unknown) => false,
);
const mockUseTPSLOrder = jest.fn(() => [
  {
    side: OrderSide.BUY,
    quantity: 0,
  },
  {
    submit: jest.fn(),
    deleteOrder: jest.fn(),
    setValue: mockSetValue,
    setValues: mockSetValues,
    validate: jest.fn(),
    metaState: {},
    errors: {},
    isCreateMutating: false,
    isUpdateMutating: false,
  },
]);

jest.mock("@paxeer-network/hooks", () => ({
  ERROR_MSG_CODES: { SL_PRICE_WARNING: 10001, SL_PRICE_ERROR: 10002 },
  useAccount: () => ({ state: { mainAccountId: "main-account" } }),
  useEstLiqPriceBySymbol: () => undefined,
  useEventEmitter: () => ({ emit: jest.fn() }),
  useLocalStorage: () => [false],
  useMarginModeBySymbol: () => ({ marginMode: mockMarginMode }),
  useMemoizedFn: (fn: unknown) => fn,
  usePositionStream: () => [{ rows: mockPositions }],
  useSymbolsInfo: () => ({ PERP_ETH_USDC: jest.fn() }),
  useTPSLOrder: (...args: unknown[]) => mockUseTPSLOrder(...args),
  useTpslPriceChecker: () => mockSlPriceError,
  utils: {
    findTPSLFromOrder: jest.fn(() => ({})),
    findTPSLOrderPriceFromOrder: jest.fn(() => ({})),
  },
}));

jest.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

jest.mock("@paxeer-network/ui", () => ({
  modal: { confirm: jest.fn() },
  toast: { error: jest.fn() },
}));

jest.mock("./positionTpslConfirm", () => ({
  PositionTPSLConfirm: () => null,
}));

jest.mock("./tpslOrderSync", () => ({
  getChangedTPSLEditableOrderValues: jest.fn(() => ({})),
  getTPSLEditableOrderValues:
    jest.requireActual("./tpslOrderSync").getTPSLEditableOrderValues,
  isTPSLOrderTypeLocked: (order: unknown, leg: unknown) =>
    mockIsTPSLOrderTypeLocked(order, leg),
}));

const isolatedPosition = {
  symbol: "PERP_ETH_USDC",
  margin_mode: MarginMode.ISOLATED,
  position_qty: 2,
  average_open_price: 4000,
};
const crossPosition = {
  symbol: "PERP_ETH_USDC",
  margin_mode: MarginMode.CROSS,
  position_qty: 3,
  average_open_price: 3900,
};

let latestResult: ReturnType<typeof useTPSLBuilder>;

const HookProbe = ({ options }: { options: TPSLBuilderOptions }) => {
  latestResult = useTPSLBuilder(options);
  return null;
};

const renderBuilder = (options: Partial<TPSLBuilderOptions> = {}) => {
  let renderer: ReactTestRenderer;
  act(() => {
    renderer = create(
      <HookProbe
        options={{
          symbol: "PERP_ETH_USDC",
          positionType: PositionType.FULL,
          close: mockClose,
          ...options,
        }}
      />,
    );
  });
  return {
    result: latestResult!,
    unmount: () => act(() => renderer!.unmount()),
  };
};

const createTPSLOrder = ({
  parentTriggered = false,
  tpTriggered = false,
  slTriggered = false,
} = {}) =>
  ({
    algo_order_id: 1,
    algo_type: AlgoOrderRootType.POSITIONAL_TP_SL,
    symbol: "PERP_ETH_USDC",
    quantity: 2,
    is_triggered: parentTriggered,
    child_orders: [
      {
        algo_order_id: 2,
        algo_type: "TAKE_PROFIT",
        type: OrderType.LIMIT,
        trigger_price: 4200,
        price: 4210,
        is_triggered: tpTriggered,
      },
      {
        algo_order_id: 3,
        algo_type: "STOP_LOSS",
        type: OrderType.LIMIT,
        trigger_price: 3800,
        price: 3790,
        is_triggered: slTriggered,
      },
    ],
  }) as any;

describe("useTPSLBuilder position selection", () => {
  beforeEach(() => {
    jest.clearAllMocks();
    mockIsTPSLOrderTypeLocked.mockReturnValue(false);
    mockMarginMode = MarginMode.ISOLATED;
    mockPositions = [isolatedPosition, crossPosition];
  });

  it("selects the current Isolated position when position is omitted", () => {
    const view = renderBuilder();

    expect(view.result.position).toBe(isolatedPosition);
    expect(mockClose).not.toHaveBeenCalled();
    expect(mockUseTPSLOrder).toHaveBeenCalledWith(
      expect.objectContaining({
        position_qty: 2,
        average_open_price: 4000,
        margin_mode: MarginMode.ISOLATED,
      }),
      expect.any(Object),
    );

    view.unmount();
  });

  it("does not close when the account only has an Isolated position", () => {
    mockPositions = [isolatedPosition];

    const view = renderBuilder();

    expect(view.result.position).toBe(isolatedPosition);
    expect(mockClose).not.toHaveBeenCalled();
    view.unmount();
  });

  it("prefers the edited order margin mode over position and symbol modes", () => {
    const view = renderBuilder({
      order: {
        symbol: "PERP_ETH_USDC",
        margin_mode: MarginMode.CROSS,
      } as any,
      position: isolatedPosition as any,
    });

    expect(view.result.position).toBe(crossPosition);
    view.unmount();
  });

  it("prefers the explicit position margin mode over the symbol mode", () => {
    const view = renderBuilder({ position: crossPosition as any });

    expect(view.result.position).toBe(crossPosition);
    view.unmount();
  });

  it("falls back to Cross when no margin mode is available", () => {
    mockMarginMode = undefined;
    const view = renderBuilder();

    expect(view.result.position).toBe(crossPosition);
    view.unmount();
  });

  it("keeps an explicit sub-account position", () => {
    const subAccountPosition = {
      ...isolatedPosition,
      account_id: "sub-account",
      position_qty: 4,
    };
    const view = renderBuilder({ position: subAccountPosition as any });

    expect(view.result.position).toBe(subAccountPosition);
    expect(mockClose).not.toHaveBeenCalled();
    view.unmount();
  });

  it("locks both order type selectors while editing", () => {
    mockIsTPSLOrderTypeLocked.mockReturnValue(true);
    const view = renderBuilder({
      order: {
        symbol: "PERP_ETH_USDC",
        child_orders: [],
      } as any,
      isEditing: true,
    });

    expect(view.result.disableTPOrderTypeSelector).toBe(true);
    expect(view.result.disableSLOrderTypeSelector).toBe(true);
    view.unmount();
  });

  it("applies the order type lock independently to each leg", () => {
    mockIsTPSLOrderTypeLocked.mockImplementation((_order, leg) => leg === "tp");
    const view = renderBuilder({
      order: {
        symbol: "PERP_ETH_USDC",
        child_orders: [],
      } as any,
      isEditing: true,
    });

    expect(view.result.disableTPOrderTypeSelector).toBe(true);
    expect(view.result.disableSLOrderTypeSelector).toBe(false);
    view.unmount();
  });

  it("locks both trigger legs and quantity when the parent is triggered", () => {
    const view = renderBuilder({
      order: createTPSLOrder({ parentTriggered: true, tpTriggered: true }),
      isEditing: true,
    });

    expect(view.result.disableTPTriggerEditing).toBe(true);
    expect(view.result.disableSLTriggerEditing).toBe(true);
    expect(view.result.disableQuantityEditing).toBe(true);
    view.unmount();
  });

  it("does not use child trigger flags to lock the editor", () => {
    const view = renderBuilder({
      order: createTPSLOrder({
        parentTriggered: false,
        tpTriggered: true,
        slTriggered: true,
      }),
      isEditing: true,
    });

    expect(view.result.disableTPTriggerEditing).toBe(false);
    expect(view.result.disableSLTriggerEditing).toBe(false);
    expect(view.result.disableQuantityEditing).toBe(false);
    view.unmount();
  });

  it("does not lock trigger controls outside edit mode", () => {
    const view = renderBuilder({
      order: createTPSLOrder({ tpTriggered: true, slTriggered: true }),
      isEditing: false,
    });

    expect(view.result.disableTPTriggerEditing).toBe(false);
    expect(view.result.disableSLTriggerEditing).toBe(false);
    view.unmount();
  });
});

describe("useTPSLBuilder historical SL liquidation risk", () => {
  const risk = {
    sl_trigger_price: {
      type: 10002,
      message: "Stop loss crosses the liq. price. Please adjust your SL.",
    },
  };
  const mockValidate = jest.fn();
  const mockSubmit = jest.fn();
  let form: any;

  beforeEach(() => {
    jest.clearAllMocks();
    mockPositions = [crossPosition];
    mockMarginMode = MarginMode.CROSS;
    mockSlPriceError = risk;
    form = {
      side: OrderSide.BUY,
      quantity: 1.532,
      tp_trigger_price: "3016",
      sl_trigger_price: "1500.00",
    };
    mockValidate.mockImplementation(async (errors) => {
      if (errors) throw errors;
      return form;
    });
    mockSubmit.mockResolvedValue({ success: true });
    mockUseTPSLOrder.mockImplementation(() => [
      form,
      {
        submit: mockSubmit,
        deleteOrder: jest.fn(),
        setValue: mockSetValue,
        setValues: mockSetValues,
        validate: mockValidate,
        metaState: {},
        errors: {},
        isCreateMutating: false,
        isUpdateMutating: false,
      },
    ]);
  });

  afterEach(() => {
    mockSlPriceError = null;
  });

  const order = (active = true) => ({
    ...createTPSLOrder(),
    algo_type: AlgoOrderRootType.TP_SL,
    quantity: 1.532,
    child_orders: [
      {
        algo_order_id: 3,
        algo_type: "STOP_LOSS",
        trigger_price: 1500,
        type: OrderType.MARKET,
        is_activated: active,
        is_triggered: false,
      },
    ],
  });

  it("allows editing TP while retaining the unchanged SL risk as a warning", async () => {
    const view = renderBuilder({ order: order(), isEditing: true });
    expect(view.result.isSlPriceWarning).toBe(true);
    expect(view.result.slPriceError).toBe(risk);
    await act(async () => {
      await expect(view.result.onSubmit()).resolves.toBe(true);
    });
    expect(mockValidate).toHaveBeenCalledWith(undefined);
    expect(mockSubmit).toHaveBeenCalledTimes(1);
    view.unmount();
  });

  it.each([
    ["changed SL", true, true, "1490"],
    ["new order", false, true, "1500"],
    ["reactivated SL", true, false, "1500"],
  ])(
    "blocks a crossed liquidation price for %s",
    async (_name, isEditing, active, price) => {
      form.sl_trigger_price = price;
      const view = renderBuilder({ order: order(active), isEditing });
      expect(view.result.isSlPriceWarning).toBe(false);
      await act(async () => {
        await expect(view.result.onSubmit()).rejects.toEqual(risk);
      });
      expect(mockValidate).toHaveBeenCalledWith(risk);
      expect(mockSubmit).not.toHaveBeenCalled();
      view.unmount();
    },
  );

  it.each(["", undefined, "NaN"])(
    "does not treat %s as the original SL",
    (price) => {
      form.sl_trigger_price = price;
      const view = renderBuilder({ order: order(), isEditing: true });
      expect(view.result.isSlPriceWarning).toBe(false);
      view.unmount();
    },
  );
});
