import React, { useState, useCallback, useMemo } from "react";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogFooter,
  DialogDescription,
} from "@/ui/atoms/dialog";
import { Button } from "@/ui/atoms/button";
import { Input } from "@/ui/atoms/input";
import { usePosition } from "@/hooks/perps/usePosition";
import { toast } from "@/ui/atoms/use-toast";
import { toastError } from "@/widgets/errors";
import { POSITION_ACTION_COPY } from "@/core/perps/config";
import { toCollateralUnits } from "@/core/perps/collateralInputs";
import { parseEther } from "viem";

type AdjustMode = "add" | "remove" | "addSize";

interface AdjustCollateralModalProps {
  positionId: string;
  mode: AdjustMode;
  currentCollateral: number;
  currentSize: number;
  currentLeverage: number;
  onClose: () => void;
  onSuccess?: () => void;
}

const MODE_TITLES: Record<AdjustMode, string> = {
  add: POSITION_ACTION_COPY.addCollateral,
  remove: POSITION_ACTION_COPY.removeCollateral,
  addSize: POSITION_ACTION_COPY.addSize,
};

export function AdjustCollateralModal({
  positionId,
  mode,
  currentCollateral,
  currentSize,
  currentLeverage,
  onClose,
  onSuccess,
}: AdjustCollateralModalProps) {
  const [amount, setAmount] = useState("");
  const [leverage, setLeverage] = useState(currentLeverage.toString());
  const [isPending, setIsPending] = useState(false);
  const positionHook = usePosition();

  const parsedAmount = useMemo(() => {
    const n = parseFloat(amount);
    return isNaN(n) || n <= 0 ? 0 : n;
  }, [amount]);

  const remainingMarginRatio = useMemo(() => {
    if (mode !== "remove" || currentSize <= 0) return null;
    const remaining = currentCollateral - parsedAmount;
    return remaining > 0 ? (remaining / currentSize) * 100 : 0;
  }, [mode, currentCollateral, currentSize, parsedAmount]);

  const handleSubmit = useCallback(async () => {
    const collateralUnits = toCollateralUnits(amount);
    if (parsedAmount <= 0 || collateralUnits <= 0n) return;
    const posId = BigInt(positionId);
    setIsPending(true);

    try {
      if (mode === "add") {
        await positionHook.addCollateral(posId, collateralUnits);
        toast({ title: POSITION_ACTION_COPY.addCollateral });
      } else if (mode === "remove") {
        const remaining = currentCollateral - parsedAmount;
        const maintenanceThreshold = currentSize * 0.01;
        if (remaining < maintenanceThreshold) {
          toast({
            title: POSITION_ACTION_COPY.errorInsufficientMargin,
            variant: "destructive",
          });
          setIsPending(false);
          return;
        }
        await positionHook.removeCollateral(posId, collateralUnits);
        toast({ title: POSITION_ACTION_COPY.successRemoveCollateral });
      } else if (mode === "addSize") {
        const lev = parseFloat(leverage);
        if (isNaN(lev) || lev <= 0) return;
        await positionHook.addSize(posId, collateralUnits, parseEther(lev.toString()));
        toast({ title: POSITION_ACTION_COPY.successAddSize });
      }
      onSuccess?.();
      onClose();
    } catch (err) {
      const errorKey = mode === "remove" ? "errorRemoveCollateral" : mode === "addSize" ? "errorAddSize" : "errorAddSize";
      toastError(err, {
        context: `position/${mode}-collateral`,
        titleOverride: POSITION_ACTION_COPY[errorKey],
        onRetry: () => { void handleSubmit(); },
      });
    } finally {
      setIsPending(false);
    }
  }, [positionId, amount, leverage, mode, parsedAmount, currentCollateral, currentSize, positionHook, onClose, onSuccess]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{MODE_TITLES[mode]}</DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground">
            Current collateral: ${currentCollateral.toLocaleString("en-US", { minimumFractionDigits: 2 })}
            {" | "}Size: ${currentSize.toLocaleString("en-US", { minimumFractionDigits: 2 })}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">
              {mode === "addSize"
                ? POSITION_ACTION_COPY.additionalCollateralLabel
                : POSITION_ACTION_COPY.amountLabel}
            </label>
            <Input
              type="number"
              step="any"
              placeholder="0.00"
              value={amount}
              onChange={(e) => setAmount(e.target.value)}
              className="text-right font-mono"
            />
          </div>

          {mode === "addSize" && (
            <div>
              <label className="text-xs text-muted-foreground mb-1 block">
                {POSITION_ACTION_COPY.leverageLabel}
              </label>
              <Input
                type="number"
                step="any"
                placeholder={currentLeverage.toString()}
                value={leverage}
                onChange={(e) => setLeverage(e.target.value)}
                className="text-right font-mono"
              />
            </div>
          )}

          {mode === "remove" && remainingMarginRatio !== null && (
            <div className="flex items-center justify-between text-xs">
              <span className="text-muted-foreground">{POSITION_ACTION_COPY.remainingMarginLabel}</span>
              <span
                className={
                  remainingMarginRatio > 10
                    ? "text-emerald-500 font-mono"
                    : remainingMarginRatio > 5
                    ? "text-amber-500 font-mono"
                    : "text-red-500 font-mono"
                }
              >
                {remainingMarginRatio.toFixed(2)}%
              </span>
            </div>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending || parsedAmount <= 0}>
            {isPending ? "Submitting..." : MODE_TITLES[mode]}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
