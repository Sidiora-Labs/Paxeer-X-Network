import { useEffect, useMemo, useState } from "react";
import {
  APIKeyItem,
  ScopeType,
  useAccount,
  useApiKeyManager,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext, useDataTap } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { toast, usePagination } from "@paxeer-network/ui";

export type GenerateKeyInfo = {
  key: string;
  secretKey: string;
  ip?: string;
  permissions?: string;
};

export const useApiManagerScript = (props?: {
  filterTags?: [string];
  keyStatus?: string;
}) => {
  const { filterTags, keyStatus } = props ?? {};
  const [showCreateDialog, setShowCreateDialog] = useState(false);
  const [showCreatedDialog, setShowCreatedDialog] = useState(false);
  const [generateKey, setGenerateKey] = useState<GenerateKeyInfo | undefined>();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const { state, account } = useAccount();
  const { t } = useTranslation();

  const canCreateApiKey =
    !wrongNetwork &&
    !disabledConnect &&
    (state.status === AccountStatusEnum.EnableTrading ||
      state.status === AccountStatusEnum.EnableTradingWithoutConnected);

  const [
    keys,
    {
      generatePaxeerNetworkKey,
      setIPRestriction,
      removePaxeerNetworkKey,
      resetPaxeerNetworkKeyIPRestriction,
      refresh,
      isLoading,
      error,
    },
  ] = useApiKeyManager({
    keyInfo: { key_status: keyStatus },
  });

  const [curPubKey, setCurPubKey] = useState<string | undefined>(undefined);

  useEffect(() => {
    account.keyStore
      .getPaxeerNetworkKey()
      ?.getPublicKey()
      .then((pubKey) => {
        setCurPubKey(pubKey);
      });
  }, [account, state]);

  const onCreateApiKey = () => {
    setShowCreateDialog(true);
  };
  const onReadApiGuide = () => {
    window.open(
      "https://paxeer-network.network/docs/build-on-omnichain/evm-api/api-authentication#api-authentication",
      "_blank",
    );
  };

  const hideCreateDialog = () => {
    setShowCreateDialog(false);
  };

  const doCreate = async (
    ipRestriction?: string,
    scope?: ScopeType,
  ): Promise<number> => {
    try {
      const createdSuccess = (
        res: {
          key: string;
          secretKey: string;
        },
        ip?: string,
      ) => {
        const { key, secretKey } = res;
        hideCreateDialog();
        setGenerateKey({
          key: key,
          secretKey,
          ip: ip,
          permissions: scope
            ?.split(",")
            ?.map((e) => capitalizeFirstChar(e))
            .join(", "),
        });
        setShowCreatedDialog(true);
      };

      const res = await generatePaxeerNetworkKey(scope);
      /// add ed25519: prefix to the key
      const generateKeyRes = {
        ...res,
        key: `ed25519:${res.key}`,
        secretKey: `ed25519:${res.secretKey}`,
      };
      toast.success(t("portfolio.apiKey.created"));

      if ((ipRestriction?.length || 0) > 0) {
        const key = generateKeyRes.key;
        const res = await setIPRestriction(key, ipRestriction!);
        if (res.success) {
          createdSuccess(
            generateKeyRes,
            res.data.ip_restriction_list?.join(","),
          );
        }
      } else {
        createdSuccess(generateKeyRes, undefined);
      }
    } catch (err: any) {
      if (err?.message) toast.error(err?.message);
    }

    return Promise.resolve(0);
  };

  const hideCreatedDialog = () => {
    setShowCreatedDialog(false);
    refresh();
  };

  const onCopyApiKeyInfo = () => {
    navigator.clipboard.writeText(JSON.stringify(generateKey));
    toast.success(t("portfolio.apiKey.apiInfo.copied"));
  };
  const doConfirm = () => {
    hideCreatedDialog();
  };

  const doDelete = (item: APIKeyItem): Promise<any> => {
    return new Promise(async (resolve) => {
      await removePaxeerNetworkKey(item.paxeerNetwork_key)
        .then(
          async (data) => {
            if (data?.success) {
              toast.success(t("portfolio.apiKey.deleted"));
              refresh();
              // delete current api key, wiil disconnect
              const curKey = await account.keyStore
                .getPaxeerNetworkKey()
                ?.getPublicKey();
              if (item.paxeerNetwork_key === curKey) {
                account.destroyPaxeerNetworkKey();
              }
            }
            resolve(1);
          },
          (reject) => {},
        )
        .catch((err) => {});
    });
  };

  const doEdit = async (item: APIKeyItem, ip?: string): Promise<void> => {
    let future;
    if ((ip?.length || 0) === 0) {
      future = resetPaxeerNetworkKeyIPRestriction(item.paxeerNetwork_key, "ALLOW_ALL_IPS");
    } else {
      future = setIPRestriction(item.paxeerNetwork_key, ip!);
    }

    const data = await future;

    if (data.success) {
      toast.success(t("portfolio.apiKey.updated"));
      refresh();
      return Promise.resolve();
    } else {
      toast.error(data.message);
    }
    return Promise.reject();
  };

  const onCopyAccountId = () =>
    toast.success(t("portfolio.apiKey.accountId.copied"));
  const onCopyApiKey = (key?: string) => {
    toast.success(t("portfolio.apiKey.column.apiKey.copy"));
  };
  const onCopyApiSecretKey = () =>
    toast.success(t("portfolio.apiKey.secretKey.copied"));
  const onCopyIP = () =>
    toast.success(t("portfolio.apiKey.column.restrictedIP.copy"));

  const keyList = useMemo(() => {
    return keys?.filter((e) => {
      const filterTag = filterTags ? filterTags?.includes(e.tag) : true;
      const filterCurKey = curPubKey
        ? !e.paxeerNetwork_key.includes(curPubKey)
        : true;
      return filterTag && filterCurKey;
    });
  }, [keys, filterTags, curPubKey]);

  const verifyIP = (ip: string) => {
    const ipRegex =
      /^((25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])(,((25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9])\.){3}(25[0-5]|2[0-4][0-9]|1[0-9]{2}|[1-9]?[0-9]))*$/;
    return ipRegex.test(ip)
      ? ""
      : "The IP restriction format is incorrect. Please use the correct format: [xx.xx.xxx.x],[xx.xxx.xxx.xxx]";
  };

  const accountStatus =
    state.status === AccountStatusEnum.EnableTradingWithoutConnected
      ? AccountStatusEnum.EnableTradingWithoutConnected
      : AccountStatusEnum.EnableTrading;

  const accountId = useDataTap(state.accountId, {
    accountStatus,
  });

  const userId = useDataTap(state.userId, {
    accountStatus,
  });

  const { pagination } = usePagination();

  return {
    accountId: accountId ?? "--",
    userId: userId ?? "--",
    onCreateApiKey,
    onReadApiGuide,
    showCreateDialog,
    hideCreateDialog,
    doCreate,
    showCreatedDialog,
    hideCreatedDialog,
    onCopyApiKeyInfo,
    doConfirm,
    doDelete,
    doEdit,
    canCreateApiKey,
    status: state.status,
    keys: keyList,
    generateKey,
    onCopyAccountId,
    wrongNetwork,
    onCopyApiKey,
    onCopyApiSecretKey,
    onCopyIP,
    verifyIP,
    isLoading,
    pagination,
  };
};

export function capitalizeFirstChar(str: string): string {
  return str.charAt(0).toUpperCase() + str.slice(1);
}

export type ApiManagerScriptReturns = ReturnType<typeof useApiManagerScript>;
