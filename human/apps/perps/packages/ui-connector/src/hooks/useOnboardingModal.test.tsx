/** @vitest-environment jsdom */
import { cloneElement, type ReactElement } from "react";
import {
  act,
  cleanup,
  render,
  renderHook,
  screen,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AccountStatusEnum } from "@paxeer-network/types";
import {
  WalletConnectorModalId,
  WalletConnectorSheetId,
} from "../component/walletConnector";
import { useOnboardingModal } from "./useOnboardingModal";

const mocks = vi.hoisted(() => ({
  accountState: {
    status: 0 as AccountStatusEnum,
  },
  isMobile: false,
  modalShow: vi.fn(),
}));

vi.mock("@paxeer-network/hooks", () => ({
  useAccount: () => ({ state: mocks.accountState }),
}));

vi.mock("@paxeer-network/i18n", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

vi.mock("@paxeer-network/ui", async (importOriginal) => {
  const original = await importOriginal<typeof import("@paxeer-network/ui")>();
  return {
    ...original,
    modal: { show: mocks.modalShow },
    useScreen: () => ({ isMobile: mocks.isMobile }),
  };
});

describe("useOnboardingModal", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.accountState.status = AccountStatusEnum.NotConnected;
    mocks.isMobile = false;
    mocks.modalShow.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
  });

  it("opens the desktop onboarding dialog with the requested status", async () => {
    const { result } = renderHook(() => useOnboardingModal());

    await act(() =>
      result.current.openOnboardingModal(AccountStatusEnum.NotSignedIn),
    );

    expect(mocks.modalShow).toHaveBeenCalledWith(
      WalletConnectorModalId,
      expect.objectContaining({
        initAccountState: AccountStatusEnum.NotSignedIn,
      }),
    );
  });

  it("opens the mobile onboarding sheet", async () => {
    mocks.isMobile = true;
    const { result } = renderHook(() => useOnboardingModal());

    await act(() =>
      result.current.openOnboardingModal(AccountStatusEnum.DisabledTrading),
    );

    expect(mocks.modalShow).toHaveBeenCalledWith(
      WalletConnectorSheetId,
      expect.objectContaining({
        initAccountState: AccountStatusEnum.DisabledTrading,
      }),
    );
  });

  it("swallows the rejection when the onboarding modal closes", async () => {
    mocks.modalShow.mockRejectedValueOnce(new Error("closed"));
    const { result } = renderHook(() => useOnboardingModal());

    await expect(
      result.current.openOnboardingModal(AccountStatusEnum.NotSignedIn),
    ).resolves.toBeUndefined();
  });

  it.each([
    AccountStatusEnum.NotSignedIn,
    AccountStatusEnum.SignedIn,
    AccountStatusEnum.DisabledTrading,
  ])("opens onboarding for account status %s", (status) => {
    const { result } = renderHook(() => useOnboardingModal());

    act(() => result.current.handleAccountStatus(status));

    expect(mocks.modalShow).toHaveBeenCalledTimes(1);
    expect(mocks.modalShow).toHaveBeenCalledWith(
      WalletConnectorModalId,
      expect.objectContaining({ initAccountState: status }),
    );
  });

  it.each([
    undefined,
    AccountStatusEnum.NotConnected,
    AccountStatusEnum.Connected,
    AccountStatusEnum.EnableTrading,
  ])("does not open onboarding for account status %s", (status) => {
    const { result } = renderHook(() => useOnboardingModal());

    act(() => result.current.handleAccountStatus(status));

    expect(mocks.modalShow).not.toHaveBeenCalled();
  });

  it("keeps the title in sync with the latest account status", async () => {
    const { result } = renderHook(() => useOnboardingModal());

    await act(() =>
      result.current.openOnboardingModal(AccountStatusEnum.NotSignedIn),
    );

    const title = mocks.modalShow.mock.calls[0][1].title as ReactElement;
    const titleView = render(cloneElement(title));
    expect(screen.getByText("connector.createAccount")).toBeTruthy();

    mocks.accountState.status = AccountStatusEnum.DisabledTrading;
    titleView.rerender(cloneElement(title));

    expect(screen.queryByText("connector.createAccount")).toBeNull();
    expect(screen.getByText("connector.enableTrading")).toBeTruthy();
  });

  it("uses the requested status while the account state catches up", async () => {
    const { result } = renderHook(() => useOnboardingModal());

    await act(() =>
      result.current.openOnboardingModal(AccountStatusEnum.DisabledTrading),
    );

    const title = mocks.modalShow.mock.calls[0][1].title as ReactElement;
    render(cloneElement(title));

    expect(screen.queryByText("connector.createAccount")).toBeNull();
    expect(screen.getByText("connector.enableTrading")).toBeTruthy();
  });
});
