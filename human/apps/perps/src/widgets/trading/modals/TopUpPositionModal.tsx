import React, { useState, useCallback } from "react";
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
import { useTopUpPosition } from "@/hooks/perps/useMarginMode";
import { toast } from "@/ui/atoms/use-toast";
import { toastError } from "@/widgets/errors";
import { POSITION_ACTION_COPY } from "@/core/perps/config";
import { toCollateralUnits } from "@/core/perps/collateralInputs";

interface TopUpPositionModalProps {
  positionId: string;
  userAddress: `0x${string}` | undefined;
  onClose: () => void;
  onSuccess?: () => void;
}

export function TopUpPositionModal({
  positionId,
  userAddress,
  onClose,
  onSuccess,
}: TopUpPositionModalProps) {
  const [amount, setAmount] = useState("");
  const { execute, isPending } = useTopUpPosition(userAddress);

  const handleSubmit = useCallback(async () => {
    const collateralUnits = toCollateralUnits(amount);
    if (collateralUnits <= 0n) return;

    try {
      await execute({ positionId: BigInt(positionId), amount: collateralUnits });
      toast({ title: POSITION_ACTION_COPY.successTopUp });
      onSuccess?.();
      onClose();
    } catch (err) {
      toastError(err, {
        context: "position/top-up",
        titleOverride: POSITION_ACTION_COPY.errorTopUp,
        onRetry: () => { void handleSubmit(); },
      });
    }
  }, [positionId, amount, execute, onClose, onSuccess]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{POSITION_ACTION_COPY.topUp}</DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground">
            Add margin to position #{positionId}
          </DialogDescription>
        </DialogHeader>

        <div>
          <label className="text-xs text-muted-foreground mb-1 block">
            {POSITION_ACTION_COPY.amountLabel}
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

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending || !amount || parseFloat(amount) <= 0}>
            {isPending ? "Submitting..." : POSITION_ACTION_COPY.topUp}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
