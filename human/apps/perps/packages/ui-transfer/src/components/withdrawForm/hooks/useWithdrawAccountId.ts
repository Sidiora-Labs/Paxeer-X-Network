import { useCallback, useEffect, useRef, useState } from "react";
import {
  useConfig,
  useDebouncedCallback,
  useInternalTransfer,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { toast } from "@paxeer-network/ui";
import { InputStatus } from "../../../types";
import { checkIsAccountId, getTransferErrorMessage } from "../../../utils";
import { validateWalletAddress } from "../withdrawForm.script";

type InternalWithdrawOptions = {
  token: string;
  decimals: number;
  quantity: string;
  setQuantity: (quantity: string) => void;
  close?: () => void;
  setLoading: (loading: boolean) => void;
};

export type AccountInfo = {
  accountId: string;
  address: string;
};

export function useWithdrawAccountId(options: InternalWithdrawOptions) {
  const { token, quantity, setQuantity, close, setLoading, decimals } = options;
  const { t } = useTranslation();
  const [toAccountId, setToAccountId] = useState<string>("");
  const [inputStatus, setInputStatus] = useState<InputStatus>("default");
  const [hintMessage, setHintMessage] = useState<string>();
  const [accountInfo, setAccountInfo] = useState<AccountInfo | null>(null);
  const [isDropdownOpen, setIsDropdownOpen] = useState(false);
  const latestLookupInputRef = useRef<string>("");

  const config = useConfig();
  const brokerId = config.get("brokerId") as string | undefined;
  const apiBaseUrl = config.get("apiBaseUrl") as string | undefined;

  const { transfer, submitting } = useInternalTransfer();

  const onTransfer = useCallback(() => {
    const num = Number(quantity);

    if (Number.isNaN(num) || num <= 0) {
      toast.error(t("transfer.quantity.invalid"));
      return;
    }

    if (submitting || !toAccountId) {
      return;
    }
    setLoading(true);

    transfer({
      token,
      receiver: toAccountId,
      amount: quantity,
      decimals,
    })
      .then(() => {
        toast.success(t("transfer.internalTransfer.success"));
        setQuantity("");
        close?.();
      })
      .catch((err) => {
        console.log("transfer error", err);
        const errorMsg = getTransferErrorMessage(err.code);
        toast.error(errorMsg);
      })
      .finally(() => {
        setLoading(false);
      });
  }, [t, quantity, token, submitting, toAccountId, transfer]);

  useEffect(() => {
    if (!toAccountId) {
      setInputStatus("default");
      setHintMessage("");
      setAccountInfo(null);
      setIsDropdownOpen(false);
      return;
    }

    if (checkIsAccountId(toAccountId)) {
      setInputStatus("default");
      setHintMessage("");
      return;
    }
  }, [toAccountId]);

  const walletLookup = async (
    input: string,
    network?: "EVM" | "SOL",
  ): Promise<AccountInfo | null> => {
    if (!network || !brokerId) return null;

    const chainType = network;
    const res = await fetch(
      `${apiBaseUrl}/v1/get_account?address=${encodeURIComponent(
        input,
      )}&broker_id=${encodeURIComponent(
        brokerId,
      )}&chain_type=${encodeURIComponent(chainType)}`,
    );
    const json = await res.json();

    if (res.ok && json?.success && json?.data?.account_id) {
      return {
        accountId: json.data.account_id,
        address: input,
      };
    }

    return null;
  };

  const accountLookup = async (input: string): Promise<AccountInfo | null> => {
    const res = await fetch(
      `${apiBaseUrl}/v1/public/account?account_id=${encodeURIComponent(input)}`,
    );
    const json = await res.json();

    if (
      res.ok &&
      json?.success &&
      json?.data?.address &&
      (!brokerId || json.data.broker_id === brokerId)
    ) {
      return {
        accountId: input,
        address: json.data.address,
      };
    }

    if (res.ok && json?.success && brokerId) {
      console.log(
        "This account belongs to a different broker and cannot be used here.",
      );
    }

    return null;
  };

  const performLookup = useCallback(
    async (rawInput: string) => {
      const input = rawInput.trim();

      if (!input || input !== latestLookupInputRef.current) {
        return;
      }

      const { valid, network } = validateWalletAddress(input);

      try {
        let resolved: AccountInfo | null = null;

        if (valid) {
          resolved = await walletLookup(input, network);
        } else {
          resolved = await accountLookup(input);
        }

        if (!resolved) {
          setAccountInfo(null);
          setIsDropdownOpen(false);
          setInputStatus("error");
          setHintMessage(t("transfer.withdraw.accountId.invalid"));
        } else {
          setAccountInfo(resolved);
          setIsDropdownOpen(true);
          setInputStatus("default");
          setHintMessage("");
        }
      } catch (error) {
        console.log("Failed to search account. Please try again.", error);
        setAccountInfo(null);
        setIsDropdownOpen(false);
        setInputStatus("error");
        setHintMessage(t("transfer.withdraw.accountId.invalid"));
      }
    },
    [brokerId, t],
  );

  const debouncedLookup = useDebouncedCallback((input: string) => {
    void performLookup(input);
  }, 500);

  const handleAccountIdChange = useCallback(
    (val: string) => {
      setToAccountId(val);
      const trimmed = val.trim();
      if (!trimmed) {
        setAccountInfo(null);
        setIsDropdownOpen(false);
        latestLookupInputRef.current = "";
        return;
      }
      latestLookupInputRef.current = trimmed;

      if (checkIsAccountId(trimmed) && accountInfo?.accountId === trimmed) {
        setIsDropdownOpen(false);
        setInputStatus("default");
        setHintMessage("");
        return;
      }
      debouncedLookup(trimmed);
    },
    [debouncedLookup, accountInfo],
  );

  return {
    toAccountId,
    setToAccountId: handleAccountIdChange,
    onTransfer,
    internalWithdrawSubmitting: submitting,
    toAccountIdInputStatus: inputStatus,
    toAccountIdHintMessage: hintMessage,
    toAccountInfo: accountInfo,
    toAccountInfoDropdownOpen: isDropdownOpen,
    setToAccountInfoDropdownOpen: setIsDropdownOpen,
  };
}
