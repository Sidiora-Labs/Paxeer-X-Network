import mockReact from "react";
import { act, create } from "react-test-renderer";
import { PNLInput } from "./pnlInput.ui";
import { PnLMode } from "./useBuilder.script";

jest.mock("./useBuilder.script", () => ({
  PnLMode: {
    PnL: "PnL",
    OFFSET: "Offset",
    PERCENTAGE: "Offset%",
    OFFSET_FROM_MARK: "OffsetFromMark",
    PERCENTAGE_FROM_MARK: "PercentageFromMark",
  },
}));

jest.mock("@paxeer-network/ui", () => ({
  CaretDownIcon: () => null,
  Input: (props: any) =>
    mockReact.createElement("input-control", props, props.suffix),
  SimpleDropdownMenu: (props: any) =>
    mockReact.createElement("dropdown-menu", props, props.children),
  Text: (props: any) => mockReact.createElement("text", props, props.children),
  cn: (...args: any[]) => args.filter(Boolean).join(" "),
  inputFormatter: {
    currencyFormatter: {},
    decimalPointFormatter: {},
  },
}));

describe("PNLInput", () => {
  it("disables both value and mode editing", () => {
    const onModeChange = jest.fn();
    const renderInput = (disabled: boolean) => (
      <PNLInput
        {...({
          disabled,
          mode: PnLMode.PERCENTAGE,
          modes: [
            { label: "Offset%", value: PnLMode.PERCENTAGE },
            { label: "PnL", value: PnLMode.PnL },
          ],
          modeLabelMap: {
            [PnLMode.PERCENTAGE]: "Offset",
          },
          onModeChange,
          onValueChange: jest.fn(),
          quote: "USDC",
          quote_dp: 2,
          value: "0.1",
          pnl: "100",
          formatter: () => ({}),
          setFocus: jest.fn(),
        } as any)}
      />
    );
    const tree = create(renderInput(true));

    expect(tree.root.findByType("input-control" as any).props.disabled).toBe(
      true,
    );
    expect(tree.root.findByType("button").props.disabled).toBe(true);
    expect(tree.root.findAllByType("dropdown-menu" as any)).toHaveLength(0);
    expect(onModeChange).not.toHaveBeenCalled();

    act(() => tree.update(renderInput(false)));

    act(() => {
      tree.root
        .findByType("dropdown-menu" as any)
        .props.onSelect({ label: "PnL", value: PnLMode.PnL });
    });
    expect(onModeChange).toHaveBeenCalledWith(PnLMode.PnL);

    act(() => tree.update(renderInput(true)));
    expect(tree.root.findAllByType("dropdown-menu" as any)).toHaveLength(0);
    expect(tree.root.findByType("button").props.disabled).toBe(true);
    tree.unmount();
  });
});
