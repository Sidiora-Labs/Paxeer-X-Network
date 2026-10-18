import { useCallback, useEffect, useMemo, useState, useRef } from "react";
import {
  useAccount,
  useIndexPricesStream,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ABSTRACT_CHAIN_ID_MAP,
  API,
  EMPTY_LIST,
  EMPTY_OBJECT,
} from "@paxeer-network/types";
import { toast, useScreen } from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { useAccountValue } from "./useAccountValue";

export type SubAccountScriptReturn = ReturnType<typeof SubAccountScript>;

type AccountValueInfo = {
  id: string;
  userAddress?: string;
  description?: string;
  holding: API.Holding[];
  accountValue?: number;
};

export const SubAccountScript = () => {
  const [open, setOpen] = useState(false);
  const [mainAccountHolding, setMainAccountHolding] = useState<API.Holding[]>(
    [],
  );
  const { wallet, connectedChain } = useWalletConnector();
  const { data: indexPrices } = useIndexPricesStream();
  const { isMobile } = useScreen();
  const { state, account, subAccount, switchAccount } = useAccount();
  const { t } = useTranslation();
  const mainAccountId = state.mainAccountId;

  const { accountValue } = useAccountValue(mainAccountId);

  const currentAccountId = state.accountId;

  const hasRefreshedRef = useRef(false);

  const userAddress = useMemo(() => {
    let address = state.address;
    if (
      connectedChain?.id &&
      ABSTRACT_CHAIN_ID_MAP.has(parseInt(connectedChain?.id as string))
    ) {
      address = account.getAdditionalInfo()?.AGWAddress;
    }

    return address;
  }, [wallet, state, account, connectedChain]);

  const subAccounts = useMemo(() => {
    if (!state.subAccounts || !state.subAccounts.length) {
      return [];
    }

    const currentSubAccount = state.subAccounts.find(
      (subAccount) => subAccount.id === currentAccountId,
    );

    if (currentSubAccount) {
      return [
        currentSubAccount,
        ...state.subAccounts.filter(
          (subAccount) => subAccount.id !== currentAccountId,
        ),
      ];
    }

    return [...state.subAccounts];
  }, [state.subAccounts, currentAccountId]);

  const _popup = useMemo(
    () => ({ mode: isMobile ? "sheet" : "modal" }),
    [isMobile],
  );

  const doCreatSubAccount = useCallback(
    (nickName: string) => {
      return subAccount.create(nickName);
    },
    [subAccount],
  );

  const onSwitch = useCallback(
    (accountId: string) => {
      return switchAccount(accountId)
        .catch((error) => {
          console.error(error);
        })
        .then((res) => {
          toast.success(t("subAccount.modal.switch.success.description"));
        });
    },
    [switchAccount, t],
  );

  const accountsWithValues = useMemo(() => {
    const mainAccountUnsettlePnl = accountValue[mainAccountId!] ?? 0;

    const mainAccount =
      mainAccountId && state.address
        ? {
            id: mainAccountId,
            userAddress: state.address,
            holding: mainAccountHolding,
            accountValue: calculateAccountValue(
              mainAccountHolding,
              mainAccountUnsettlePnl,
              indexPrices || EMPTY_OBJECT,
            ),
          }
        : undefined;

    const updatedSubAccounts = subAccounts.map((subAccount) => {
      const subAccountUnsettlePnl = accountValue[subAccount.id] ?? 0;
      return {
        ...subAccount,
        accountValue: calculateAccountValue(
          subAccount.holding || EMPTY_LIST,
          subAccountUnsettlePnl,
          indexPrices || EMPTY_OBJECT,
        ),
      };
    });

    return {
      mainAccount,
      subAccounts: updatedSubAccounts,
    };
  }, [
    mainAccountId,
    state.address,
    mainAccountHolding,
    subAccounts,
    accountValue,
    indexPrices,
  ]);

  useEffect(() => {
    if (!open) {
      hasRefreshedRef.current = false;
      return;
    }

    if (!hasRefreshedRef.current && mainAccountId) {
      hasRefreshedRef.current = true;
      subAccount.refresh().then((res) => {
        setMainAccountHolding(res[mainAccountId] || []);
      });
    }
  }, [open, mainAccountId, subAccount]);

  return {
    userAddress,
    mainAccount: accountsWithValues.mainAccount,
    currentAccountId,
    open,
    onOpenChange: setOpen,
    popup: _popup,
    createSubAccount: doCreatSubAccount,
    subAccounts: accountsWithValues.subAccounts,
    onSwitch,
  };
};

const calculateAccountValue = (
  holdings: API.Holding[],
  unsettlePnl: number,
  indexPrices: Record<string, number>,
) => {
  const holding = holdings.reduce((acc, holding) => {
    const price = getTokenIndexPrice(holding.token, indexPrices);
    if (!price) {
      return acc;
    }
    return new Decimal(holding.holding)
      .times(price)
      .add(holding.isolated_margin ?? 0)
      .add(acc)
      .toNumber();
  }, 0);
  return holding + unsettlePnl;
};

const getTokenIndexPrice = (
  token: string,
  indexPrices: Record<string, number>,
) => {
  if (token === "USDC") {
    return 1;
  }
  const symbol = `PERP_${token}_USDC`;
  return indexPrices[symbol] ?? 0;
};
