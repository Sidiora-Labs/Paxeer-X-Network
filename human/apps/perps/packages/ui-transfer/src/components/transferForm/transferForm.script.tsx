import { useCallback, useEffect, useMemo, useState } from "react";
import {
  SubAccount,
  useAccount,
  useConfig,
  useSubAccountDataObserver,
  useSubAccountMaxWithdrawal,
  useTransfer,
} from "@paxeer-network/hooks";
import { useAppStore } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, NetworkId } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { Decimal, zero } from "@paxeer-network/utils";
import { InputStatus } from "../../types";
import { getTransferErrorMessage } from "../../utils";
import { useSettlePnl } from "../unsettlePnlInfo/useSettlePnl";

export type TransferFormScriptReturn = ReturnType<typeof useTransferFormScript>;

export type TransferFormScriptOptions = {
  /** target sub account id */
  toAccountId?: string;
  token?: string;
  close?: () => void;
};

const DEFAULT_TOKEN = {
  symbol: "USDC",
} as API.TokenInfo;

export const useTransferFormScript = (options: TransferFormScriptOptions) => {
  const { t } = useTranslation();
  const [quantity, setQuantity] = useState<string>("");
  const [fromAccount, setFromAccount] = useState<SubAccount>();
  const [toAccount, setToAccount] = useState<SubAccount>();
  const [mainAccount, setMainAccount] = useState<SubAccount>();
  const [tokens, setTokens] = useState<API.TokenInfo[]>([DEFAULT_TOKEN]);
  const [token, setToken] = useState<API.TokenInfo>(DEFAULT_TOKEN);
  const [holdingMap, setHoldingMap] = useState<Record<string, API.Holding[]>>(
    {},
  );

  const networkId = useConfig("networkId") as NetworkId;

  const { state, isMainAccount, subAccount } = useAccount();

  const tokensInfo = useAppStore((state) => state.tokensInfo);

  const {
    transfer,
    submitting,
    maxAmount: currentMaxAmount,
    unsettledPnL: currentUnsettledPnL,
  } = useTransfer({ fromAccountId: fromAccount?.id, token: token.symbol });

  const subAccounts = state.subAccounts;
  const mainAccountId = state.mainAccountId;
  const accountId = state.accountId;

  // when current account is main account, and fromAccount is not main account, set observerAccountId to fromAccountId
  // current sub account use main account paxeer-network key to transfer, so fromAccount can be main account or current sub account
  const observerAccountId = isMainAccount
    ? fromAccount?.id !== mainAccountId
      ? fromAccount?.id
      : undefined
    : fromAccount?.id;

  const { hasPositions: currentHasPositions, onSettlePnl } = useSettlePnl({
    accountId: fromAccount?.id,
  });

  // when select sub account, open the private websocket
  const { portfolio, positions } = useSubAccountDataObserver(observerAccountId);
  const subAccountMaxAmount = useSubAccountMaxWithdrawal({
    token: token.symbol,
    unsettledPnL: portfolio?.unsettledPnL,
    freeCollateral: portfolio?.freeCollateral ?? zero,
    holdings: portfolio?.holding,
  });

  const formHasPositions = useMemo(
    () => !!positions?.rows?.length,
    [positions],
  );

  const hasPositions = useMemo(() => {
    return observerAccountId ? formHasPositions : currentHasPositions;
  }, [observerAccountId, formHasPositions, currentHasPositions]);

  const { unsettledPnL, maxQuantity } = useMemo(() => {
    if (observerAccountId) {
      return {
        unsettledPnL: portfolio?.unsettledPnL,
        maxQuantity: subAccountMaxAmount,
      };
    }
    return {
      unsettledPnL: currentUnsettledPnL,
      maxQuantity: currentMaxAmount,
    };
  }, [
    observerAccountId,
    currentUnsettledPnL,
    currentMaxAmount,
    portfolio?.unsettledPnL,
    subAccountMaxAmount,
  ]);

  const { inputStatus, hintMessage } = useInputStatus({
    quantity,
    maxQuantity,
    unsettledPnL,
  });

  const onTransfer = useCallback(() => {
    const num = Number(quantity);

    if (isNaN(num) || num <= 0) {
      toast.error(t("transfer.quantity.invalid"));
      return;
    }

    if (submitting || !toAccount) return;

    transfer(token.symbol!, {
      account_id: toAccount.id,
      amount: new Decimal(quantity).toNumber(),
    })
      .then(() => {
        toast.success(t("transfer.internalTransfer.success"));
        setQuantity("");
        options.close?.();
      })
      .catch((err) => {
        console.error("transfer error: ", err);
        const errorMsg = getTransferErrorMessage(err.code);
        toast.error(errorMsg);
      });
  }, [t, token, quantity, submitting, transfer, toAccount]);

  const disabled = submitting || !quantity || inputStatus === "error";

  const amount = useMemo(() => {
    const markPrice = 1;
    return new Decimal(quantity || 0).mul(markPrice).toNumber();
  }, [quantity]);

  const toAccountAsset = useMemo(() => {
    if (!toAccount?.id) {
      return 0;
    }
    const holdings = holdingMap[toAccount.id];
    const asset = holdings?.find((item) => item.token === token.symbol);
    return asset?.holding || 0;
  }, [toAccount, token, holdingMap]);

  const { fromAccounts, toAccounts } = useMemo(() => {
    if (isMainAccount) {
      return {
        fromAccounts: mainAccount ? [mainAccount, ...(subAccounts || [])] : [],
        toAccounts:
          // only from account is main account, can transfer to other sub accounts
          fromAccount?.id === mainAccountId ? subAccounts : [],
      };
    }

    return {
      fromAccounts: [],
      toAccounts: [],
    };
  }, [isMainAccount, mainAccountId, mainAccount, subAccounts, fromAccount]);

  // init and update main account holding
  useEffect(() => {
    if (!mainAccountId) return;

    const _mainAccount = {
      id: mainAccountId!,
      description: t("common.mainAccount"),
      holding: [],
    };

    setMainAccount(_mainAccount);

    subAccount.refresh().then((res) => {
      setHoldingMap(res);
      setMainAccount({
        ..._mainAccount,
        holding: res[mainAccountId],
      });
    });
  }, [t, mainAccountId]);

  // init fromAccount
  useEffect(() => {
    // if current account is main account, set fromAccount to main account
    // if current account is sub account, set fromAccount to current sub account
    setFromAccount(
      isMainAccount
        ? mainAccount
        : subAccounts?.find((item) => item.id === accountId),
    );
  }, [isMainAccount, mainAccount, subAccounts, accountId]);

  // init toAccount
  useEffect(() => {
    // if current account is main account
    if (isMainAccount) {
      const firstSubAccount = subAccounts?.[0];

      // if toAccount is not set, set toAccount to first sub account
      const selectAccount = options.toAccountId
        ? subAccounts?.find((item) => item.id === options.toAccountId) ||
          firstSubAccount
        : firstSubAccount;

      if (selectAccount) {
        setToAccount(selectAccount);
      }
    } else {
      // if current account is sub account, set toAccount to main account
      setToAccount(mainAccount);
    }
  }, [options?.toAccountId, isMainAccount, mainAccount, subAccounts]);

  useEffect(() => {
    const tokens = tokensInfo?.map((item) => ({
      symbol: item.token,
      precision: item.decimals,
    })) as API.TokenInfo[];

    if (tokens?.length) {
      // sort tokens, USDC should be the first
      tokens.sort((a, b) => {
        if (a.symbol === "USDC") return -1;
        if (b.symbol === "USDC") return 1;
        return 0;
      });

      const targetToken = tokens?.find((item) => item.symbol === options.token);

      setTokens(tokens);
      setToken(targetToken || tokens?.[0] || DEFAULT_TOKEN);
    }
  }, [tokensInfo, options.token]);

  const onFromAccountChange = useCallback(
    (account: SubAccount) => {
      setFromAccount(account);

      const firstSubAccount = subAccounts?.[0];
      // if fromAccount is main account, set toAccount to first sub account
      if (account?.id === mainAccountId) {
        setToAccount(firstSubAccount);
        return;
      }

      // if fromAccount is not main account, set toAccount to main account
      // sub account only can transfer to main account
      if (account?.id && account?.id !== mainAccountId) {
        setToAccount(mainAccount);
        return;
      }
    },
    [mainAccountId, mainAccount, subAccounts],
  );

  const onToAccountChange = useCallback((account: SubAccount) => {
    setToAccount(account);
  }, []);

  const onExchange = useCallback(() => {
    setFromAccount(toAccount);
    setToAccount(fromAccount);
  }, [fromAccount, toAccount]);

  return {
    networkId,
    onTransfer,
    quantity,
    amount,
    onQuantityChange: setQuantity,
    maxQuantity,
    tokens,
    token,
    onTokenChange: setToken,
    disabled,
    submitting,
    hintMessage,
    inputStatus,
    hasPositions,
    onSettlePnl,
    unsettledPnL,
    toAccountAsset,
    fromAccount,
    toAccount,
    fromAccounts,
    onFromAccountChange,
    toAccounts,
    onToAccountChange,
    onExchange,
    isMainAccount,
  };
};

type Options = {
  quantity: string;
  maxQuantity: string | number;
  unsettledPnL?: number;
};

export function useInputStatus(options: Options) {
  const { quantity, maxQuantity, unsettledPnL = 0 } = options;
  const { t } = useTranslation();

  const [inputStatus, setInputStatus] = useState<InputStatus>("default");
  const [hintMessage, setHintMessage] = useState<string>();

  useEffect(() => {
    if (!quantity) {
      // reset input status when value is empty
      setInputStatus("default");
      setHintMessage("");
      return;
    }

    const qty = new Decimal(quantity);

    if (unsettledPnL < 0) {
      if (qty.gt(maxQuantity)) {
        setInputStatus("error");
        setHintMessage(t("transfer.insufficientBalance"));
      } else {
        setInputStatus("default");
        setHintMessage("");
      }
    } else {
      if (qty.gt(maxQuantity)) {
        setInputStatus("error");
        setHintMessage(t("transfer.insufficientBalance"));
      } else if (
        qty.gt(new Decimal(maxQuantity).minus(unsettledPnL)) &&
        qty.lessThanOrEqualTo(maxQuantity)
      ) {
        setInputStatus("warning");
        setHintMessage(t("settle.settlePnl.warning"));
      } else {
        // reset input status
        setInputStatus("default");
        setHintMessage("");
      }
    }
  }, [maxQuantity, quantity, t, unsettledPnL]);

  return { inputStatus, hintMessage };
}
