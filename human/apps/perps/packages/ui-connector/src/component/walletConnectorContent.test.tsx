/** @vitest-environment jsdom */
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AccountStatusEnum, ChainNamespace } from "@paxeer-network/types";
import { ActionButton, WalletConnectContent } from "./walletConnectorContent";

const mocks = vi.hoisted(() => ({
  setManualLedgerAddress: vi.fn(),
  accountState: {
    address: "address-a",
    status: 2 as AccountStatusEnum,
  },
}));

vi.mock("@paxeer-network/hooks", () => ({
  formatReferralCodeInput: (value: string) => value,
  REFERRAL_CODE_MAX_LENGTH: 20,
  REFERRAL_CODE_MIN_LENGTH: 1,
  useAccount: () => ({
    state: mocks.accountState,
    account: { address: "address-a" },
  }),
  useEventEmitter: () => ({ emit: vi.fn() }),
  useLocalStorage: () => [undefined, vi.fn()],
  useStorageLedgerAddress: () => ({
    setManualLedgerAddress: mocks.setManualLedgerAddress,
  }),
  useWalletConnector: () => ({}),
}));

vi.mock("@paxeer-network/i18n", () => ({
  i18n: { t: (key: string) => key },
  useTranslation: () => ({ t: (key: string) => key }),
}));

describe("ActionButton Ledger mode", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.accountState.status = AccountStatusEnum.NotSignedIn;
  });

  afterEach(() => {
    cleanup();
  });

  it("keeps the current Ledger mode when the main button is the only action", () => {
    const signIn = vi.fn().mockResolvedValue(undefined);
    const onUseStandardWallet = vi.fn();

    render(
      <ActionButton
        state={AccountStatusEnum.NotSignedIn}
        signIn={signIn}
        enableTrading={vi.fn()}
        onUseStandardWallet={onUseStandardWallet}
        loading={false}
        showLedgerButton={false}
        adapterName="Phantom"
        namespace={ChainNamespace.solana}
      />,
    );

    fireEvent.click(screen.getByText("connector.createAccount"));

    expect(onUseStandardWallet).not.toHaveBeenCalled();
    expect(signIn).toHaveBeenCalledTimes(1);
  });

  it("keeps the current Ledger mode when enabling trading", () => {
    const enableTrading = vi.fn().mockResolvedValue(undefined);
    const onUseStandardWallet = vi.fn();

    render(
      <ActionButton
        state={AccountStatusEnum.SignedIn}
        signIn={vi.fn().mockResolvedValue(undefined)}
        enableTrading={enableTrading}
        onUseStandardWallet={onUseStandardWallet}
        loading={false}
        showLedgerButton={false}
        adapterName="Phantom"
        namespace={ChainNamespace.solana}
      />,
    );

    fireEvent.click(screen.getByText("connector.enableTrading"));

    expect(onUseStandardWallet).not.toHaveBeenCalled();
    expect(enableTrading).toHaveBeenCalledTimes(1);
  });

  it("uses standard signing only when both signing actions are available", () => {
    const signIn = vi.fn().mockResolvedValue(undefined);
    const onUseStandardWallet = vi.fn();

    render(
      <ActionButton
        state={AccountStatusEnum.NotSignedIn}
        signIn={signIn}
        enableTrading={vi.fn()}
        onUseStandardWallet={onUseStandardWallet}
        loading={false}
        showLedgerButton
        adapterName="Phantom"
        namespace={ChainNamespace.solana}
      />,
    );

    fireEvent.click(screen.getByText("connector.createAccount"));

    expect(onUseStandardWallet).toHaveBeenCalledTimes(1);
    expect(signIn).toHaveBeenCalledTimes(1);
  });

  it("writes a manual override before invoking the Ledger action", () => {
    const signIn = vi.fn().mockResolvedValue(undefined);

    render(
      <ActionButton
        state={AccountStatusEnum.NotSignedIn}
        signIn={signIn}
        enableTrading={vi.fn()}
        onUseStandardWallet={vi.fn()}
        loading={false}
        showLedgerButton
        adapterName="Phantom"
        namespace={ChainNamespace.solana}
      />,
    );

    fireEvent.click(screen.getByText("connector.createAccountWithLedger"));

    expect(mocks.setManualLedgerAddress).toHaveBeenCalledWith(
      "address-a",
      "Phantom",
    );
    expect(
      mocks.setManualLedgerAddress.mock.invocationCallOrder[0],
    ).toBeLessThan(signIn.mock.invocationCallOrder[0]);
  });

  it("hides the Ledger action when the adapter name is unavailable", () => {
    const signIn = vi.fn().mockResolvedValue(undefined);
    const onUseStandardWallet = vi.fn();

    render(
      <ActionButton
        state={AccountStatusEnum.NotSignedIn}
        signIn={signIn}
        enableTrading={vi.fn()}
        onUseStandardWallet={onUseStandardWallet}
        loading={false}
        showLedgerButton
        namespace={ChainNamespace.solana}
      />,
    );

    expect(screen.queryByText("connector.createAccountWithLedger")).toBeNull();

    fireEvent.click(screen.getByText("connector.createAccount"));

    expect(onUseStandardWallet).not.toHaveBeenCalled();
    expect(signIn).toHaveBeenCalledTimes(1);
    expect(mocks.setManualLedgerAddress).not.toHaveBeenCalled();
  });

  it("renders the requested onboarding step before account state catches up", () => {
    render(
      <WalletConnectContent
        initAccountState={AccountStatusEnum.DisabledTrading}
        signIn={vi.fn()}
        enableTrading={vi.fn()}
        refCode=""
        setRefCode={vi.fn()}
        showRefCodeInput={false}
      />,
    );

    expect(screen.queryByText("connector.createAccount")).toBeNull();
    expect(screen.getAllByText("connector.enableTrading")).toHaveLength(2);
  });

  it("advances the visible step when the live account state moves forward", () => {
    mocks.accountState.status = AccountStatusEnum.DisabledTrading;

    render(
      <WalletConnectContent
        initAccountState={AccountStatusEnum.NotSignedIn}
        signIn={vi.fn()}
        enableTrading={vi.fn()}
        refCode=""
        setRefCode={vi.fn()}
        showRefCodeInput={false}
      />,
    );

    const createAccountStep = screen.getByText("connector.createAccount");
    const enableTradingStep = screen.getAllByText("connector.enableTrading")[0];

    expect(
      createAccountStep.parentElement?.parentElement?.querySelector("svg"),
    ).not.toBeNull();
    expect(
      enableTradingStep.parentElement?.parentElement?.querySelector(
        ".oui-bg-primary-light",
      ),
    ).not.toBeNull();
  });
});
