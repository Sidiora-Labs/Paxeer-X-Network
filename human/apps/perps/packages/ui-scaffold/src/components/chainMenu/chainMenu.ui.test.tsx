/** @vitest-environment jsdom */
import React from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { ChainMenu } from "./chainMenu.ui";

const mocks = vi.hoisted(() => ({
  modalShow: vi.fn(),
}));

vi.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@paxeer-network/ui", () => ({
  Button: ({ children, onClick }: any) => (
    <button onClick={onClick}>{children}</button>
  ),
  Tooltip: ({ children }: any) => <>{children}</>,
  modal: { show: mocks.modalShow },
}));

vi.mock("@paxeer-network/ui-chain-selector", () => ({
  ChainSelectorDialogId: "ChainSelectorDialogId",
  ChainSelectorWidget: () => null,
}));

describe("ChainMenu wrong network onboarding", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("passes chain validation callbacks to the wrong-network selector", async () => {
    mocks.modalShow.mockResolvedValue({ wrongNetwork: false });
    const onChainChangeBefore = vi.fn();
    const onChainChangeAfter = vi.fn();

    render(
      <ChainMenu
        {...({
          wrongNetwork: true,
          isConnected: true,
          disabledConnect: false,
          networkId: "mainnet",
          onChainChangeBefore,
          onChainChangeAfter,
        } as any)}
      />,
    );

    fireEvent.click(screen.getByRole("button"));
    await act(async () => {
      await Promise.resolve();
    });

    expect(mocks.modalShow).toHaveBeenCalledWith("ChainSelectorDialogId", {
      networkId: "mainnet",
      onChainChangeBefore,
      onChainChangeAfter,
    });
  });
});
