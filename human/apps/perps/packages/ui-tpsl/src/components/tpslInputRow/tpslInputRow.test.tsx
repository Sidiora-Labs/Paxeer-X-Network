import mockReact from "react";
import { act, create } from "react-test-renderer";
import { OrderType, PositionType } from "@paxeer-network/types";
import { TPSLInputRowUI } from "./tpslInputRow.ui";

jest.mock("@paxeer-network/hooks", () => ({}));
jest.mock("@paxeer-network/react-app", () => ({
  useOrderEntryFormErrorMsg: () => ({ getErrorMsg: () => undefined }),
}));
jest.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
  Trans: ({ components }: any) => <>{components}</>,
}));
jest.mock("@paxeer-network/ui", () => {
  const box = ({ children }: any) =>
    mockReact.createElement("div", null, children);
  box.numeral = ({ children }: any) =>
    mockReact.createElement("numeral", null, children);
  return {
    Flex: box,
    Text: box,
    Grid: box,
    cn: (...args: any[]) => args.filter(Boolean).join(" "),
  };
});
jest.mock("../../pnlInput/pnlInput.widget", () => ({
  PnlInputWidget: (props: any) => mockReact.createElement("pnl-input", props),
}));
jest.mock("../orderPriceType", () => ({
  OrderPriceType: (props: any) => mockReact.createElement("order-type", props),
}));
jest.mock("./priceInput", () => ({
  PriceInput: (props: any) => mockReact.createElement("price-input", props),
}));

const props: any = {
  type: "tp",
  quote_dp: 2,
  positionType: PositionType.FULL,
  values: {
    trigger_price: "4100",
    order_price: "4110",
    order_type: OrderType.LIMIT,
    PnL: "110",
  },
  errors: null,
  roi: 10,
  onChange: jest.fn(),
};

describe("Full-position TP/SL row", () => {
  it.each(["tp", "sl"])(
    "enables independent %s Limit input and type selection",
    (type) => {
      const tree = create(<TPSLInputRowUI {...props} type={type} />);
      const inputs = tree.root.findAllByType("price-input" as any);
      expect(inputs[1].props.disabled).toBe(false);
      expect(
        tree.root.findByType("order-type" as any).props.disabled,
      ).toBeFalsy();
      act(() => inputs[1].props.onValueChange("4120"));
      expect(props.onChange).toHaveBeenCalledWith(
        `${type}_order_price`,
        "4120",
      );
      tree.unmount();
    },
  );
  it("locks only type while editing a server Limit leg", () => {
    const tree = create(<TPSLInputRowUI {...props} disableOrderTypeSelector />);
    expect(tree.root.findByType("order-type" as any).props.disabled).toBe(true);
    expect(
      tree.root.findAllByType("price-input" as any)[1].props.disabled,
    ).toBe(false);
    tree.unmount();
  });
  it("locks the trigger but keeps the Limit order price and Offset editable", () => {
    props.onChange.mockClear();
    const tree = create(
      <TPSLInputRowUI
        {...props}
        disableOrderTypeSelector
        disableTriggerEditing
      />,
    );
    const inputs = tree.root.findAllByType("price-input" as any);

    expect(inputs[0].props.disabled).toBe(true);
    expect(inputs[1].props.disabled).toBe(false);
    expect(tree.root.findByType("pnl-input" as any).props.disabled).toBe(false);
    expect(tree.root.findByType("order-type" as any).props.disabled).toBe(true);

    act(() => inputs[0].props.onValueChange("4200"));
    expect(props.onChange).not.toHaveBeenCalled();

    act(() => inputs[1].props.onValueChange("4120"));
    expect(props.onChange).toHaveBeenCalledWith("tp_order_price", "4120");
    tree.unmount();
  });
  it.each([
    { order_type: OrderType.MARKET, trigger_price: "4100" },
    { order_type: OrderType.LIMIT, trigger_price: "" },
    { order_type: OrderType.LIMIT, trigger_price: undefined },
  ])("locks Offset when it could change a locked trigger: %j", (values) => {
    const tree = create(
      <TPSLInputRowUI
        {...props}
        disableTriggerEditing
        values={{ ...props.values, ...values }}
      />,
    );
    expect(tree.root.findByType("pnl-input" as any).props.disabled).toBe(true);
    tree.unmount();
  });
  it("keeps Market order price disabled but permits changing new order type", () => {
    const tree = create(
      <TPSLInputRowUI
        {...props}
        values={{ ...props.values, order_type: OrderType.MARKET }}
      />,
    );
    expect(
      tree.root.findAllByType("price-input" as any)[1].props.disabled,
    ).toBe(true);
    expect(
      tree.root.findByType("order-type" as any).props.disabled,
    ).toBeFalsy();
    tree.unmount();
  });
  it("keeps trigger wording independent of the Limit price used for PnL", () => {
    const tree = create(<TPSLInputRowUI {...props} />);
    const numbers = tree.root
      .findAllByType("numeral" as any)
      .map((node) => node.props.children);
    expect(numbers).toEqual(["4100", "110", 10]);
    tree.unmount();
  });
});
