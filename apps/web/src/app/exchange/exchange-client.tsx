"use client";

import { exchange as perpsVenue } from "@paxeer/wallet";
import { useRouter } from "next/navigation";
import { useState, useTransition } from "react";

import {
  perpsCancelOrderCall,
  perpsClosePositionCall,
  perpsDepositCall,
  perpsOpenPositionCall,
  perpsWithdrawCall,
  waitLaunchpadApproval,
  type PrecompileCall,
} from "../../api/sdk";
import { sendWalletPrecompileCall, walletFeePreference, walletSendFailure, type PrecompileSendOutcome } from "../../api/wallet";
import { windowWalletProvider } from "../../journeys/custody/handoff";
import { SegmentedControl } from "../../kit/collection";
import { KitButton } from "../../kit/control";
import { ExplorerPanel } from "../../kit/explorer";
import { TextField } from "../../kit/field";
import { availability } from "../_markets/availability";
import { WEI_DECIMALS, parseUnits } from "../_markets/format";
import { ChoiceField } from "../_markets/select";
import { SendOutcome } from "../_markets/send-outcome";
import { SPONSORED_APPROVE_UNSUPPORTED } from "../launchpad/launchpad-client";
import { preflightPerpsWrite } from "./actions";

export type PerpsCollateral = Readonly<{ token: string; decimals: number; symbol: string }>;
type Choice = Readonly<{ id: string; label: string }>;

// 0x1015 pulls deposit and openPosition collateral by transferFrom, so a
// short allowance gets one approve for the exact amount, mined first. The
// write is then run against the precompile so its revert reason is shown
// before the wallet opens.
export async function sendPerpsWrite(
  from: string,
  collateral: PerpsCollateral,
  pull: bigint,
  call: PrecompileCall,
  onApproving: (active: boolean) => void,
): Promise<PrecompileSendOutcome> {
  const wallet = windowWalletProvider();
  if (wallet === undefined) {
    return { outcome: "unavailable" };
  }
  try {
    const approve = pull > 0n ? await perpsVenue(wallet).approval(from, collateral.token, pull) : undefined;
    if (approve !== undefined) {
      if (walletFeePreference().currency === "sidiora") {
        return { outcome: "failed", detail: SPONSORED_APPROVE_UNSUPPORTED };
      }
      onApproving(true);
      const approved = await sendWalletPrecompileCall(from, () => approve);
      if (approved.outcome !== "sent") {
        return approved;
      }
      await waitLaunchpadApproval(wallet, approved.transactionHash);
    }
    const preflight = await preflightPerpsWrite(from, call.data);
    if (!preflight.ok) {
      return { outcome: "failed", detail: preflight.detail };
    }
  } catch (error) {
    return walletSendFailure(error);
  } finally {
    onApproving(false);
  }
  return sendWalletPrecompileCall(from, () => call);
}

export function ExchangeActions({
  account,
  collateral,
  markets,
  positions,
  orders,
}: Readonly<{
  account: string | undefined;
  collateral: PerpsCollateral;
  markets: readonly Choice[];
  positions: readonly Choice[];
  orders: readonly Choice[];
}>) {
  const router = useRouter();
  const [pending, startTransition] = useTransition();
  const [approving, setApproving] = useState(false);
  const [marketId, setMarketId] = useState(markets[0]?.id ?? "");
  const [side, setSide] = useState("long");
  const [collateralText, setCollateralText] = useState("");
  const [leverageText, setLeverageText] = useState("");
  const [openOutcome, setOpenOutcome] = useState<PrecompileSendOutcome | undefined>(undefined);
  const [positionId, setPositionId] = useState(positions[0]?.id ?? "");
  const [closeOutcome, setCloseOutcome] = useState<PrecompileSendOutcome | undefined>(undefined);
  const [orderId, setOrderId] = useState(orders[0]?.id ?? "");
  const [cancelOutcome, setCancelOutcome] = useState<PrecompileSendOutcome | undefined>(undefined);
  const [marginText, setMarginText] = useState("");
  const [marginOutcome, setMarginOutcome] = useState<PrecompileSendOutcome | undefined>(undefined);

  const amount = parseUnits(collateralText, collateral.decimals);
  const leverage = parseUnits(leverageText, WEI_DECIMALS);
  const margin = parseUnits(marginText, collateral.decimals);
  const walletReason = account === undefined ? "Connect a wallet first." : undefined;

  const send = (call: PrecompileCall, pull: bigint, record: (outcome: PrecompileSendOutcome) => void) => {
    if (account === undefined) {
      return;
    }
    startTransition(async () => {
      const outcome = await sendPerpsWrite(account, collateral, pull, call, setApproving);
      record(outcome);
      if (outcome.outcome === "sent") {
        router.refresh();
      }
    });
  };

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <ExplorerPanel title="Open a position">
        <div className="flex flex-col gap-3">
          {markets.length === 0 ? (
            <p className="text-sm text-muted-foreground">No market is listed.</p>
          ) : (
            <ChoiceField
              label="Market"
              value={marketId}
              onChange={setMarketId}
              options={markets.map((market) => ({ value: market.id, label: market.label }))}
            />
          )}
          <SegmentedControl
            aria-label="Side"
            value={side}
            onValueChange={setSide}
            options={[
              { value: "long", label: "Long" },
              { value: "short", label: "Short" },
            ]}
          />
          <TextField
            label={`Collateral (${collateral.symbol})`}
            inputMode="decimal"
            value={collateralText}
            onChange={(event) => {
              setCollateralText(event.target.value);
            }}
          />
          <TextField
            label="Leverage (x)"
            inputMode="decimal"
            value={leverageText}
            onChange={(event) => {
              setLeverageText(event.target.value);
            }}
          />
          <p className="text-xs text-muted-foreground">
            {approving
              ? `Approving the exact ${collateral.symbol} amount for the perps venue.`
              : `If the perps venue allowance is short, one approve for the exact ${collateral.symbol} amount is sent first.`}
          </p>
          <KitButton
            loading={pending}
            onClick={() => {
              if (amount === undefined || leverage === undefined) {
                return;
              }
              send(
                perpsOpenPositionCall({
                  marketId: BigInt(marketId),
                  collateralToken: collateral.token,
                  collateralAmount: amount,
                  leverage,
                  isLong: side === "long",
                }),
                amount,
                setOpenOutcome,
              );
            }}
            {...availability(
              walletReason,
              (marketId === "" || amount === undefined || amount === 0n || leverage === undefined || leverage === 0n) &&
                "Choose a market and enter collateral and leverage.",
            )}
          >
            Open position
          </KitButton>
          <SendOutcome outcome={openOutcome} />
        </div>
      </ExplorerPanel>
      <ExplorerPanel title="Close a position">
        <div className="flex flex-col gap-3">
          {positions.length === 0 ? (
            <p className="text-sm text-muted-foreground">No open position.</p>
          ) : (
            <ChoiceField
              label="Position"
              value={positionId}
              onChange={setPositionId}
              options={positions.map((position) => ({ value: position.id, label: position.label }))}
            />
          )}
          <p className="text-xs text-muted-foreground">
            Closes the whole position with a reduce-only market order at the venue slippage bound.
          </p>
          <KitButton
            variant="destructive"
            loading={pending}
            onClick={() => {
              send(perpsClosePositionCall(BigInt(positionId)), 0n, setCloseOutcome);
            }}
            {...availability(walletReason, positionId === "" && "Choose a position.")}
          >
            Close position
          </KitButton>
          <SendOutcome outcome={closeOutcome} />
        </div>
      </ExplorerPanel>
      <ExplorerPanel title="Cancel an order">
        <div className="flex flex-col gap-3">
          {orders.length === 0 ? (
            <p className="text-sm text-muted-foreground">No active order.</p>
          ) : (
            <ChoiceField
              label="Order"
              value={orderId}
              onChange={setOrderId}
              options={orders.map((order) => ({ value: order.id, label: order.label }))}
            />
          )}
          <KitButton
            variant="destructive"
            loading={pending}
            onClick={() => {
              send(perpsCancelOrderCall(BigInt(orderId)), 0n, setCancelOutcome);
            }}
            {...availability(walletReason, orderId === "" && "Choose an order.")}
          >
            Cancel order
          </KitButton>
          <SendOutcome outcome={cancelOutcome} />
        </div>
      </ExplorerPanel>
      <ExplorerPanel title="Margin">
        <div className="flex flex-col gap-3">
          <TextField
            label={`Amount (${collateral.symbol})`}
            inputMode="decimal"
            value={marginText}
            onChange={(event) => {
              setMarginText(event.target.value);
            }}
          />
          <KitButton
            loading={pending}
            onClick={() => {
              if (margin === undefined) {
                return;
              }
              send(perpsDepositCall(collateral.token, margin), margin, setMarginOutcome);
            }}
            {...availability(walletReason, (margin === undefined || margin === 0n) && "Enter an amount.")}
          >
            Deposit
          </KitButton>
          <KitButton
            variant="secondary"
            loading={pending}
            onClick={() => {
              if (margin === undefined) {
                return;
              }
              send(perpsWithdrawCall(collateral.token, margin), 0n, setMarginOutcome);
            }}
            {...availability(walletReason, (margin === undefined || margin === 0n) && "Enter an amount.")}
          >
            Withdraw
          </KitButton>
          <SendOutcome outcome={marginOutcome} />
        </div>
      </ExplorerPanel>
    </div>
  );
}
