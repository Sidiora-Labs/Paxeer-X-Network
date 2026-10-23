"use client";

import { CircleAlertIcon, CircleCheckIcon, InfoIcon, ShieldCheckIcon, TriangleAlertIcon } from "lucide-react";
import { useMemo, useState } from "react";
import { isAddress, type Hex } from "viem";
import { useAccount, useChainId, useConnect, useSwitchChain, useWaitForTransactionReceipt, useWriteContract } from "wagmi";

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Separator } from "@/components/ui/separator";
import { toast } from "@/components/ui/sonner";
import { Spinner } from "@/components/ui/spinner";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { capPercent, formatAmountDisplay, hasExcessPrecision, parseAmount } from "@/lib/bridge/amount";
import {
  BRIDGE_CHAINS,
  bridgeChainByKey,
  finalityEstimate,
  paxeer,
  PAXEER_ROUTE_KEY,
  type BridgeChain,
} from "@/lib/bridge/chains";
import { erc20Abi, layerxBridgeAbi, LAYERX_BRIDGE_ADDRESS, paxeerXVaultAbi } from "@/lib/bridge/contracts";
import { paxeerRecipient } from "@/lib/bridge/digests";
import { describeWriteError } from "@/lib/bridge/errors";
import { isSolanaAddress, solanaRecipientHandle } from "@/lib/bridge/solana";
import {
  paxeerConfigured,
  useBridgedBalance,
  useForeignBalance,
  usePaxeerPairState,
  useVaultState,
} from "@/lib/bridge/use-bridge-state";
import { AmountInput, AssetPicker, ReceivePreview, RoutePanel, SwapDirectionButton, type RouteOption } from "./route-panels";

type Direction = "in" | "out";

const ROUTE_HINT: Record<BridgeChain["state"], string> = {
  configured: "Vault deployed",
  "not-deployed": "Vault not deployed yet",
  "awaiting-backend": "Solana program + backend pending",
};

function useBridgeWrite() {
  const write = useWriteContract();
  const receipt = useWaitForTransactionReceipt({ hash: write.data });
  return {
    write,
    hash: write.data,
    isSigning: write.isPending,
    isConfirming: write.data !== undefined && receipt.isLoading,
    confirmed: receipt.isSuccess,
    reset: write.reset,
  };
}

export function BridgeCard({ onTransferSubmitted }: { onTransferSubmitted: () => void }) {
  const [direction, setDirection] = useState<Direction>("in");
  const [foreignKey, setForeignKey] = useState("base");
  const [assetKey, setAssetKey] = useState<string | undefined>(undefined);
  const [amount, setAmount] = useState("");
  const [destOverride, setDestOverride] = useState("");
  const [destTouched, setDestTouched] = useState(false);
  const [approveHash, setApproveHash] = useState<Hex | undefined>(undefined);

  const { address, isConnected } = useAccount();
  const walletChainId = useChainId();
  const { connectors, connect, isPending: connecting } = useConnect();
  const { switchChain, isPending: switching } = useSwitchChain();

  const foreign = bridgeChainByKey(foreignKey) ?? BRIDGE_CHAINS[0];
  const asset = foreign.assets.find((a) => a.key === assetKey) ?? foreign.assets[0];

  const paxeerState = usePaxeerPairState(foreign, asset.assetId);
  const vaultState = useVaultState(foreign, asset.address);
  const foreignBalance = useForeignBalance(foreign, asset.address, asset.isNative);
  const bridgedBalance = useBridgedBalance(paxeerState.denom);
  const approveReceipt = useWaitForTransactionReceipt({ hash: approveHash });

  const deposit = useBridgeWrite();
  const withdraw = useBridgeWrite();

  const parsedAmount = parseAmount(amount, asset.decimals);
  const amountValue = parsedAmount ?? 0n;
  const excessPrecision = hasExcessPrecision(amount, asset.decimals);

  const destination = destTouched ? destOverride.trim() : (address ?? "");
  const isSolanaRoute = foreign.kind === "solana";
  const destinationValid =
    direction === "in" || !isSolanaRoute
      ? isAddress(destination, { strict: false })
      : isSolanaAddress(destination);
  const destinationIsSelf =
    direction === "in"
      ? address !== undefined && destination.toLowerCase() === address.toLowerCase()
      : address !== undefined && !isSolanaRoute && destination.toLowerCase() === address.toLowerCase();

  const sourceChainId = direction === "in" ? foreign.viemChain?.id : paxeer.id;
  const onSourceChain = isConnected && sourceChainId !== undefined && walletChainId === sourceChainId;
  const vaultMatches = !paxeerConfigured || !paxeerState.loaded || !paxeerState.registered || !foreign.vault || !paxeerState.vault
    ? true
    : paxeerState.vault.toLowerCase() === foreign.vault.toLowerCase();

  const balanceForDirection = direction === "in" ? foreignBalance.data?.value : bridgedBalance.data;
  const overBalance = balanceForDirection !== undefined && amountValue > balanceForDirection;
  const vaultPerTx = vaultState.loaded ? vaultState.perTx : BigInt(asset.perTxCap);
  const overVaultPerTx = foreign.vault !== undefined && amountValue > vaultPerTx && vaultPerTx > 0n;
  const vaultTotal = vaultState.loaded ? vaultState.total : BigInt(asset.totalCap);
  const overVaultTotal =
    direction === "in" &&
    foreign.vault !== undefined &&
    vaultState.loaded &&
    vaultTotal > 0n &&
    vaultState.outstanding + amountValue > vaultTotal;
  const overPaxeerPerTx =
    paxeerConfigured && paxeerState.loaded && paxeerState.registered && paxeerState.maxPerTx > 0n && amountValue > paxeerState.maxPerTx;
  const overInFlight =
    direction === "in" &&
    paxeerConfigured &&
    paxeerState.loaded &&
    paxeerState.registered &&
    paxeerState.maxInFlight > 0n &&
    paxeerState.inFlight + amountValue > paxeerState.maxInFlight;

  const blockers = useMemo<string[]>(() => {
    const list: string[] = [];
    if (foreign.state === "not-deployed") {
      list.push(`The ${foreign.label} vault is not deployed yet. This route opens once the deployment record is published.`);
    }
    if (foreign.state === "awaiting-backend") {
      list.push(
        direction === "in"
          ? "Deposits from Solana need a Solana wallet and the deployed custody program — its program id is still a placeholder in the bridge configuration."
          : "Withdrawals to Solana also need the bridge backend to relay your full Solana address to the custody program. That backend is not connected yet.",
      );
    }
    if (!paxeerConfigured) {
      list.push("The Paxeer RPC is not configured for this deployment, so the bridge cannot be verified on-chain here.");
    } else if (paxeerState.loaded) {
      if (paxeerState.paused) list.push("The bridge is paused on Paxeer. No deposits or withdrawals will be accepted until it reopens.");
      if (!paxeerState.registered) list.push(`${foreign.label} is not registered with the Paxeer bridge yet. Governance registers a chain after its vault is deployed.`);
      else if (!paxeerState.enabled) list.push(`${foreign.label} is registered but currently disabled on Paxeer.`);
      if (!vaultMatches) list.push("The vault registered on Paxeer for this chain does not match the vault this app is configured with. Do not deposit.");
    }
    if (foreign.vault && vaultState.loaded && vaultState.paused) {
      list.push(`The vault on ${foreign.label} is paused.`);
    }
    return list;
  }, [direction, foreign, paxeerState.enabled, paxeerState.loaded, paxeerState.paused, paxeerState.registered, vaultMatches, vaultState.loaded, vaultState.paused]);

  const inputError = useMemo<string | undefined>(() => {
    if (amount.trim() === "") return undefined;
    if (excessPrecision) return `${asset.symbol} supports at most ${asset.decimals} decimal places.`;
    if (parsedAmount === undefined) return "Enter a valid amount.";
    if (amountValue === 0n) return "Enter an amount greater than zero.";
    if (overBalance) return `That is more than your ${direction === "in" ? foreign.label : "bridged"} balance.`;
    if (overVaultPerTx) return `Above the vault per-transaction cap of ${formatAmountDisplay(vaultPerTx, asset.decimals)} ${asset.symbol}.`;
    if (overVaultTotal) return "That would exceed the vault's total capacity for this asset.";
    if (overPaxeerPerTx) return `Above the Paxeer per-transaction cap of ${formatAmountDisplay(paxeerState.maxPerTx, asset.decimals)} ${asset.symbol}.`;
    if (overInFlight) return "That would exceed the bridge's in-flight capacity for this asset right now.";
    return undefined;
  }, [amount, amountValue, asset.decimals, asset.symbol, direction, excessPrecision, foreign.label, overBalance, overInFlight, overPaxeerPerTx, overVaultPerTx, overVaultTotal, parsedAmount, paxeerState.maxPerTx, vaultPerTx]);

  const destinationError =
    destination === ""
      ? undefined
      : !destinationValid
        ? direction === "in"
          ? "Enter a valid Paxeer (EVM) address."
          : isSolanaRoute
            ? "Enter a valid Solana address."
            : `Enter a valid ${foreign.label} address.`
        : undefined;

  const routeOptions: RouteOption[] = [
    { key: PAXEER_ROUTE_KEY, name: "Paxeer", hint: paxeerConfigured ? "Chain 125 · precompile 0x…1016" : "RPC not configured" },
    ...BRIDGE_CHAINS.map((c) => ({ key: c.key, name: c.label, hint: ROUTE_HINT[c.state] })),
  ];
  const sourceKey = direction === "in" ? foreign.key : PAXEER_ROUTE_KEY;
  const destKey = direction === "in" ? PAXEER_ROUTE_KEY : foreign.key;
  const optionFor = (key: string) => routeOptions.find((o) => o.key === key) ?? routeOptions[0];

  const selectRoute = (side: "source" | "destination") => (key: string) => {
    if (key === PAXEER_ROUTE_KEY) {
      setDirection(side === "source" ? "out" : "in");
      return;
    }
    setForeignKey(key);
    setAssetKey(undefined);
    setDestTouched(false);
    setDestOverride("");
    setDirection(side === "source" ? "in" : "out");
  };

  const busy = deposit.isSigning || deposit.isConfirming || withdraw.isSigning || withdraw.isConfirming || approveReceipt.isLoading;
  const ready =
    isConnected &&
    onSourceChain &&
    blockers.length === 0 &&
    amountValue > 0n &&
    parsedAmount !== undefined &&
    !inputError &&
    !destinationError &&
    destination !== "" &&
    !busy;

  const needsApprove = direction === "in" && !asset.isNative && foreign.vault !== undefined;

  const fire = (request: Parameters<typeof deposit.write.writeContract>[0], which: "deposit" | "withdraw") => {
    const w = which === "deposit" ? deposit.write : withdraw.write;
    w.writeContract(request, {
      onSuccess: () => {
        toast.success(which === "deposit" ? "Deposit submitted" : "Withdrawal burn submitted", {
          description:
            which === "deposit"
              ? `Attestors credit it on Paxeer after finality (${finalityEstimate(foreign)}). Track it under Transfers.`
              : `Attestors release it on ${foreign.label} once the burn is final. Track it under Transfers.`,
        });
        onTransferSubmitted();
      },
      onError: (error) => toast.error(which === "deposit" ? "Deposit failed" : "Withdrawal failed", { description: describeWriteError(error) }),
    });
  };

  const submit = () => {
    if (!ready || !address) return;
    if (direction === "in") {
      if (!foreign.vault || !foreign.viemChain) return;
      if (!isAddress(destination, { strict: false })) return;
      const recipient32 = paxeerRecipient(destination as `0x${string}`);
      if (asset.isNative) {
        fire(
          {
            abi: paxeerXVaultAbi,
            address: foreign.vault,
            functionName: "depositNative",
            args: [recipient32],
            value: amountValue,
            chainId: foreign.viemChain.id,
          },
          "deposit",
        );
      } else if (needsApprove && !approveReceipt.isSuccess) {
        deposit.write.writeContract(
          {
            abi: erc20Abi,
            address: asset.address,
            functionName: "approve",
            args: [foreign.vault, amountValue],
            chainId: foreign.viemChain.id,
          },
          {
            onSuccess: (hash) => setApproveHash(hash),
            onError: (error) => toast.error("Approval failed", { description: describeWriteError(error) }),
          },
        );
      } else {
        fire(
          {
            abi: paxeerXVaultAbi,
            address: foreign.vault,
            functionName: "deposit",
            args: [asset.address, amountValue, recipient32],
            chainId: foreign.viemChain.id,
          },
          "deposit",
        );
      }
    } else {
      const recipient = isSolanaRoute ? solanaRecipientHandle(destination) : (destination as `0x${string}`);
      if (!recipient) return;
      fire(
        {
          abi: layerxBridgeAbi,
          address: LAYERX_BRIDGE_ADDRESS,
          functionName: "bridgeOut",
          args: [foreign.chainId, asset.assetId, amountValue, recipient],
          chainId: paxeer.id,
        },
        "withdraw",
      );
    }
  };

  const primaryLabel = (() => {
    if (!isConnected) return "Connect wallet";
    if (blockers.length > 0) return direction === "in" ? "Deposit unavailable" : "Withdrawal unavailable";
    if (!onSourceChain && sourceChainId !== undefined) {
      return `Switch to ${direction === "in" ? foreign.label : "Paxeer"}`;
    }
    if (approveHash && approveReceipt.isLoading) return "Confirming approval…";
    if (needsApprove && !approveReceipt.isSuccess) return `Approve ${asset.symbol}`;
    if (deposit.isSigning || withdraw.isSigning) return "Confirm in wallet…";
    if (deposit.isConfirming || withdraw.isConfirming) return "Confirming…";
    return direction === "in" ? `Bridge to Paxeer` : `Bridge to ${foreign.label}`;
  })();

  const primaryAction = () => {
    if (!isConnected) {
      const connector = connectors[0];
      if (connector) {
        connect(
          { connector },
          { onError: (error) => toast.error("Could not connect", { description: describeWriteError(error) }) },
        );
      }
      return;
    }
    if (isConnected && !onSourceChain && sourceChainId !== undefined) {
      switchChain(
        { chainId: sourceChainId },
        { onError: (error) => toast.error("Could not switch network", { description: describeWriteError(error) }) },
      );
      return;
    }
    submit();
  };

  const finality = paxeerState.finalityDepth ?? foreign.finalityDepth;

  return (
    <Card variant="elevated" className="w-full gap-5">
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle>Bridge</CardTitle>
          <Badge variant={foreign.state === "configured" ? "success" : "warning"}>{ROUTE_HINT[foreign.state]}</Badge>
          {paxeerState.threshold !== undefined && paxeerState.attestors.length > 0 ? (
            <Badge variant="tonal">
              <ShieldCheckIcon /> {paxeerState.threshold} of {paxeerState.attestors.length} attestors
            </Badge>
          ) : null}
        </div>
        <CardDescription>
          Lock on one chain, mint or release on the other — every crossing is attested by the bridge attestor set.
        </CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 px-6">
        <div className="flex items-center justify-between gap-2">
          <AssetPicker
            label="Asset"
            symbols={foreign.assets.map((a) => a.symbol)}
            value={asset.symbol}
            onChange={(symbol) => {
              const next = foreign.assets.find((a) => a.symbol === symbol);
              setAssetKey(next?.key);
            }}
          />
          <Badge variant="outline" className="font-mono text-[11px]">
            1 {asset.symbol} = 1 bridged {asset.symbol}
          </Badge>
        </div>

        <RoutePanel
          label="From"
          option={optionFor(sourceKey)}
          options={routeOptions}
          onSelect={selectRoute("source")}
          pickerTitle="Bridge from"
        >
          <AmountInput
            id="bridge-amount"
            value={amount}
            onChange={setAmount}
            symbol={asset.symbol}
            decimals={asset.decimals}
            balance={balanceForDirection}
            balanceLoading={direction === "in" ? foreignBalance.isLoading : bridgedBalance.isLoading}
            invalid={inputError !== undefined}
            disabled={busy}
            onMax={
              balanceForDirection !== undefined
                ? () => setAmount(formatAmountDisplay(balanceForDirection, asset.decimals).replace(/,/g, ""))
                : undefined
            }
          />
        </RoutePanel>

        <SwapDirectionButton
          disabled={busy}
          onClick={() => {
            setDirection((d) => (d === "in" ? "out" : "in"));
            setDestTouched(false);
            setDestOverride("");
          }}
        />

        <RoutePanel
          label="To"
          option={optionFor(destKey)}
          options={routeOptions}
          onSelect={selectRoute("destination")}
          pickerTitle="Bridge to"
        >
          <ReceivePreview symbol={asset.symbol} amountText={amount.trim() || undefined} denom={direction === "in" ? paxeerState.denom : undefined} />
        </RoutePanel>

        <Field>
          <FieldLabel htmlFor="bridge-destination">
            Destination address
            {destinationIsSelf && isConnected ? (
              <Badge variant="tonal">Your wallet</Badge>
            ) : destination && destinationValid && isConnected ? (
              <Badge variant="warning">Different address — double-check it</Badge>
            ) : null}
          </FieldLabel>
          <Input
            id="bridge-destination"
            value={destination}
            onChange={(e) => {
              setDestTouched(true);
              setDestOverride(e.target.value);
            }}
            placeholder={
              direction === "in"
                ? "0x… Paxeer address"
                : isSolanaRoute
                  ? "Solana address (base58)"
                  : `0x… ${foreign.label} address`
            }
            aria-invalid={destinationError !== undefined || undefined}
            autoComplete="off"
            spellCheck={false}
            className="font-mono text-[13px]"
          />
          <FieldDescription>
            {direction === "in"
              ? "Credited on Paxeer as the bridged denom. The low 20 bytes of this address are what the attestors sign."
              : isSolanaRoute
                ? "The burn records a handle of this pubkey — the payout can only ever go to this exact Solana address."
                : `Released from the ${foreign.label} vault to this address once the burn is attested.`}
          </FieldDescription>
          {destinationError ? (
            <p role="alert" className="flex items-center gap-1.5 text-[13px] font-medium text-destructive">
              <CircleAlertIcon className="size-3.5" /> {destinationError}
            </p>
          ) : null}
        </Field>

        {inputError ? (
          <p role="alert" className="flex items-center gap-1.5 text-[13px] font-medium text-destructive">
            <CircleAlertIcon className="size-3.5" /> {inputError}
          </p>
        ) : null}

        {blockers.map((blocker) => (
          <Alert key={blocker} variant="warning">
            <TriangleAlertIcon />
            <AlertTitle>Not available yet</AlertTitle>
            <AlertDescription>{blocker}</AlertDescription>
          </Alert>
        ))}

        <Separator />

        <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-[13px] sm:grid-cols-4">
          <div>
            <dt className="text-muted-foreground">Arrival</dt>
            <dd className="font-medium">{finalityEstimate({ ...foreign, finalityDepth: finality })}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Confirmations</dt>
            <dd className="font-medium tabular-nums">{finality.toLocaleString()}</dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Per-tx cap</dt>
            <dd className="font-medium tabular-nums">
              {formatAmountDisplay(vaultPerTx, asset.decimals)} {asset.symbol}
            </dd>
          </div>
          <div>
            <dt className="text-muted-foreground">Bridge fee</dt>
            <dd className="font-medium">None · gas only</dd>
          </div>
        </dl>

        {paxeerConfigured && paxeerState.loaded && paxeerState.registered && paxeerState.maxInFlight > 0n ? (
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center justify-between text-[11px] text-muted-foreground">
              <span className="flex items-center gap-1">
                In flight to Paxeer
                <Tooltip>
                  <TooltipTrigger aria-label="About the in-flight cap" className="focus-ring rounded-full">
                    <InfoIcon className="size-3.5" />
                  </TooltipTrigger>
                  <TooltipContent variant="outline" className="max-w-60 font-normal">
                    Deposits credited on Paxeer that have not been burned back. New deposits stop at the cap.
                  </TooltipContent>
                </Tooltip>
              </span>
              <span className="font-mono tabular-nums">
                {formatAmountDisplay(paxeerState.inFlight, asset.decimals)} / {formatAmountDisplay(paxeerState.maxInFlight, asset.decimals)} {asset.symbol}
              </span>
            </div>
            <div
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={Math.round(capPercent(paxeerState.inFlight, paxeerState.maxInFlight))}
              aria-label="In-flight capacity used"
              className="h-1.5 overflow-hidden rounded-full bg-container-high"
            >
              <div
                className="h-full rounded-full bg-primary transition-[width] duration-300"
                style={{ width: `${capPercent(paxeerState.inFlight, paxeerState.maxInFlight)}%` }}
              />
            </div>
          </div>
        ) : null}

        <Button
          size="lg"
          className="w-full"
          disabled={isConnected ? (!onSourceChain && sourceChainId !== undefined ? switching : !ready) : connecting}
          onClick={primaryAction}
        >
          {busy ? <Spinner /> : null} {primaryLabel}
        </Button>

        {deposit.hash || withdraw.hash ? (
          <Alert variant="success">
            <CircleCheckIcon />
            <AlertTitle>Submitted</AlertTitle>
            <AlertDescription>
              Track finality, attestation and the credit or release below under Transfers. You can close this page —
              the transfer stays recoverable from the source transaction.
            </AlertDescription>
          </Alert>
        ) : null}
      </CardContent>
    </Card>
  );
}
