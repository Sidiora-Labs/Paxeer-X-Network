import { useEffect } from "react";
import {
  useAccount,
  useEventEmitter,
  useStorageLedgerAddress,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { ChainNamespace } from "@paxeer-network/types";
import { modal, toast } from "@paxeer-network/ui";

export function useWalletConnectError() {
  const { t } = useTranslation();
  const ee = useEventEmitter();
  const { state } = useAccount();
  const { wallet, namespace } = useWalletConnector();
  const { setManualLedgerAddress } = useStorageLedgerAddress();
  const adapterName = wallet?.label ?? state.connectWallet?.name;

  useEffect(() => {
    const handleConnectError = (data: { message: string }) => {
      toast.error(data.message);
    };

    const handleLedgerError = (data: {
      userAddress: string;
      message: string;
    }) => {
      if (namespace !== ChainNamespace.solana) {
        return;
      }

      if (!adapterName) {
        console.error(
          "Unable to enable Ledger signing without a Solana adapter name",
          { userAddress: data.userAddress },
        );
        toast.error(data.message);
        return;
      }

      window.setTimeout(() => {
        modal
          .confirm({
            title: t("connector.ledger.signMessageFailed"),
            content: t("connector.ledger.signMessageFailed.description"),
            size: "sm",
            onOk: async () => {
              console.log("-- use ledger", true);
              setManualLedgerAddress(data.userAddress, adapterName);

              return Promise.resolve();
            },
            okLabel: t("common.ok"),
            onCancel: async () => {
              toast.error(data.message);
              return Promise.resolve();
            },
            cancelLabel: t("common.no"),
          })
          .then((res) => {
            console.log("-- dialog res", res);
          });
      });
    };

    ee.on("wallet:connect-error", handleConnectError);
    ee.on("wallet:sign-message-with-ledger-error", handleLedgerError);

    return () => {
      ee.off("wallet:connect-error", handleConnectError);
      ee.off("wallet:sign-message-with-ledger-error", handleLedgerError);
    };
  }, [adapterName, ee, namespace, setManualLedgerAddress, t]);

  return {};
}
