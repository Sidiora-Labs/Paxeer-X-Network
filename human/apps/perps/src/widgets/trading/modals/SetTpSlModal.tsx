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
import { writeContract, waitForTx } from "@/hooks/perps/useContract";
import { toast } from "@/ui/atoms/use-toast";
import { toastError } from "@/widgets/errors";
import { TP_SL_COPY } from "@/core/perps/config";
import { setTpSlAction } from "@/core/perps/positionActions";

interface SetTpSlModalProps {
  positionId: string;
  currentSize: number;
  currentEntryPrice: number;
  isLong: boolean;
  markPrice: number;
  onClose: () => void;
  onSuccess?: () => void;
}

export function SetTpSlModal({
  positionId,
  currentEntryPrice,
  isLong,
  markPrice,
  onClose,
  onSuccess,
}: SetTpSlModalProps) {
  const [tpPrice, setTpPrice] = useState("");
  const [slPrice, setSlPrice] = useState("");
  const [isPending, setIsPending] = useState(false);
  const [validationError, setValidationError] = useState<string | null>(null);

  const validate = useCallback((): boolean => {
    const tp = tpPrice ? parseFloat(tpPrice) : null;
    const sl = slPrice ? parseFloat(slPrice) : null;

    if (tp === null && sl === null) {
      setValidationError(TP_SL_COPY.validationAtLeastOne);
      return false;
    }

    if (tp !== null && markPrice > 0) {
      if (isLong && tp <= markPrice) {
        setValidationError(TP_SL_COPY.validationTpLong);
        return false;
      }
      if (!isLong && tp >= markPrice) {
        setValidationError(TP_SL_COPY.validationTpShort);
        return false;
      }
    }

    if (sl !== null && markPrice > 0) {
      if (isLong && sl >= markPrice) {
        setValidationError(TP_SL_COPY.validationSlLong);
        return false;
      }
      if (!isLong && sl <= markPrice) {
        setValidationError(TP_SL_COPY.validationSlShort);
        return false;
      }
    }

    setValidationError(null);
    return true;
  }, [tpPrice, slPrice, isLong, markPrice]);

  const handleSubmit = useCallback(async () => {
    if (!validate()) return;
    setIsPending(true);
    try {
      await waitForTx(await writeContract(setTpSlAction(BigInt(positionId), isLong, tpPrice, slPrice)));
      const title = tpPrice && slPrice ? "Take Profit and Stop Loss set" : tpPrice ? TP_SL_COPY.successTp : TP_SL_COPY.successSl;
      toast({ title });
      onSuccess?.();
      onClose();
    } catch (err) {
      const titleOverride =
        tpPrice && !slPrice
          ? TP_SL_COPY.errorTp
          : slPrice && !tpPrice
            ? TP_SL_COPY.errorSl
            : "TP/SL order failed";
      toastError(err, {
        context: "position/set-tp-sl",
        titleOverride,
        onRetry: () => { void handleSubmit(); },
      });
    } finally {
      setIsPending(false);
    }
  }, [positionId, isLong, tpPrice, slPrice, validate, onClose, onSuccess]);

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="max-w-sm">
        <DialogHeader>
          <DialogTitle>{TP_SL_COPY.modalTitle}</DialogTitle>
          <DialogDescription className="text-xs text-muted-foreground">
            {isLong ? "Long" : "Short"} position at ${currentEntryPrice.toLocaleString("en-US", { minimumFractionDigits: 2 })}
            {markPrice > 0 && (
              <span className="ml-2">
                Mark: ${markPrice.toLocaleString("en-US", { minimumFractionDigits: 2 })}
              </span>
            )}
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">
              {TP_SL_COPY.tpLabel}
            </label>
            <Input
              type="number"
              step="any"
              placeholder={TP_SL_COPY.tpPlaceholder}
              value={tpPrice}
              onChange={(e) => {
                setTpPrice(e.target.value);
                setValidationError(null);
              }}
              className="text-right font-mono"
            />
          </div>
          <div>
            <label className="text-xs text-muted-foreground mb-1 block">
              {TP_SL_COPY.slLabel}
            </label>
            <Input
              type="number"
              step="any"
              placeholder={TP_SL_COPY.slPlaceholder}
              value={slPrice}
              onChange={(e) => {
                setSlPrice(e.target.value);
                setValidationError(null);
              }}
              className="text-right font-mono"
            />
          </div>

          {validationError && (
            <p className="text-xs text-red-500">{validationError}</p>
          )}
        </div>

        <DialogFooter>
          <Button variant="outline" size="sm" onClick={onClose} disabled={isPending}>
            Cancel
          </Button>
          <Button size="sm" onClick={handleSubmit} disabled={isPending}>
            {isPending ? "Submitting..." : TP_SL_COPY.submitLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
