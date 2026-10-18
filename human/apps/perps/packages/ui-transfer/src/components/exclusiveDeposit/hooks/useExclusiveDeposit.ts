import { useMemo } from "react";
import { usePrivateQuery } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";

type ReceiverAddressResponse = {
  receiver_address: string;
  token_address: string;
  minimum_deposit: number;
};

type ReceiverEvent = {
  chain_id: string;
  tx_id: string;
  token: string;
  amount: number;
  created_time: number;
  updated_time: number;
};

export type ExclusiveDepositState = {
  address?: string;
  qrUri?: string;
  minimumDeposit?: number;
  estimatedArrivalText: string;
  latestEvent?: ReceiverEvent;
  pendingCount: number;
  explorerUrl?: string;
  isAddressLoading: boolean;
  isEventsLoading: boolean;
  addressError?: unknown;
  eventsError?: unknown;
};

const buildExplorerUrl = (baseUrl: string, txId: string) => {
  const url = baseUrl;

  if (url.endsWith("/")) {
    return `${url}tx/${txId}`;
  }

  return `${url}/tx/${txId}`;
};

export const useExclusiveDeposit = (options: {
  active?: boolean;
  confirmed?: boolean;
  chainId?: number;
  token?: string;
  explorerBaseUrl: string;
}): ExclusiveDepositState => {
  const active = options.active ?? true;
  const confirmed = options.confirmed ?? true;
  const { chainId, token, explorerBaseUrl } = options;

  const { t: t0 } = useTranslation();
  const t = t0 as any;

  const receiverAddressKey =
    confirmed && chainId && token
      ? `/v1/client/asset/receiver_address?chain_id=${chainId}&token=${token}`
      : null;
  const receiverEventsKey =
    active && confirmed ? "/v1/client/asset/receiver_events" : null;

  const {
    data: addressData,
    isLoading: isAddressLoading,
    error: addressError,
  } = usePrivateQuery<ReceiverAddressResponse>(receiverAddressKey, {
    revalidateOnFocus: false,
  });

  const {
    data: eventsData,
    isLoading: isEventsLoading,
    error: eventsError,
  } = usePrivateQuery<ReceiverEvent[]>(receiverEventsKey, {
    refreshInterval: active ? 10_000 : 0,
    revalidateOnFocus: false,
  });

  const { latestEvent, pendingCount, explorerUrl } = useMemo(() => {
    const events = eventsData ?? [];

    if (!events.length) {
      return {
        latestEvent: undefined,
        pendingCount: 0,
        explorerUrl: undefined,
      };
    }

    const latest = events.reduce<ReceiverEvent | undefined>((acc, curr) => {
      if (!acc) return curr;
      return curr.created_time > acc.created_time ? curr : acc;
    }, undefined);

    const count = events.length;

    const url = latest?.tx_id
      ? buildExplorerUrl(explorerBaseUrl, latest.tx_id)
      : undefined;

    return {
      latestEvent: latest,
      pendingCount: count,
      explorerUrl: url,
    };
  }, [explorerBaseUrl, eventsData]);

  return {
    address: addressData?.receiver_address,
    qrUri: addressData?.receiver_address,
    minimumDeposit: addressData?.minimum_deposit,
    estimatedArrivalText: t("transfer.exclusiveDeposit.estimatedTime.default"),
    latestEvent,
    pendingCount,
    explorerUrl,
    isAddressLoading,
    isEventsLoading,
    addressError,
    eventsError,
  };
};
