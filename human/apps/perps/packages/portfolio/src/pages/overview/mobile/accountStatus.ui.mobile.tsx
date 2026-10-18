import { FC, SVGProps, useCallback, useMemo } from "react";
import {
  parseJSON,
  useAccount,
  useChains,
  useLocalStorage,
  useWalletConnector,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum, ChainNamespace } from "@paxeer-network/types";
import {
  cn,
  modal,
  toast,
  formatAddress,
  Flex,
  CopyIcon,
  ChainIcon,
  InfoFillIcon,
} from "@paxeer-network/ui";

interface StatusInfo {
  title: string;
  description: string;
  titleColor?: any;
  titleClsName?: string;
}

const useCurrentStatus = () => {
  const { state, account } = useAccount();
  const { wrongNetwork, disabledConnect, connectWallet } = useAppContext();
  const { t } = useTranslation();

  const statusText = {
    wrongNetwork: {
      description: t("connector.wrongNetwork.tooltip"),
      rootClass:
        "oui-bg-[linear-gradient(15deg,rgb(var(--oui-color-warning-darken))_0%,transparent_30%)]",
      info: false,
    },
    connectWallet: {
      description: t("connector.trade.connectWallet.tooltip"),
      // Informational treatment: quiet brand tint with an info glyph.
      rootClass: "oui-bg-[rgb(var(--oui-color-primary-light)/0.06)]",
      info: true,
    },
    notSignedIn: {
      description: t("connector.trade.createAccount.tooltip"),
      rootClass:
        "oui-bg-[linear-gradient(15deg,rgb(var(--oui-color-primary-darken))_0%,transparent_30%)]",
      info: false,
    },
    disabledTrading: {
      description: t("connector.trade.enableTrading.tooltip"),
      rootClass:
        "oui-bg-[linear-gradient(15deg,rgb(var(--oui-color-primary-darken))_0%,transparent_30%)]",
      info: false,
    },
    default: {
      description: "",
      rootClass: "",
      info: false,
      arrowIconClass: "",
      textClass: "",
    },
  };

  return useMemo(() => {
    if (disabledConnect) {
      return statusText.connectWallet;
    }

    if (wrongNetwork) {
      return statusText.wrongNetwork;
    }

    switch (state.status) {
      case AccountStatusEnum.NotConnected:
        return statusText.connectWallet;
      case AccountStatusEnum.NotSignedIn:
        return statusText.notSignedIn;
      case AccountStatusEnum.DisabledTrading:
        return statusText.disabledTrading;
      default:
        return statusText.default;
    }
  }, [state.status, wrongNetwork]);
};

export function EVMChainPopover({ children }: { children: React.ReactNode }) {
  // const [chains] = useState(getChainsByNetwork("mainnet"));
  const [chains] = useChains("mainnet", {
    pick: "network_infos",
  });
  const { t } = useTranslation();

  const showDialog = () => {
    modal.dialog({
      title: t("connector.privy.supportedEvmChain"),
      size: "xs",
      content: (
        <div>
          <div className="oui-p-1 oui-grid oui-grid-cols-2 oui-gap-x-2 oui-gap-y-3 oui-text-2xs oui-text-base-contrast-54">
            {chains.map((item, key) => (
              <div
                key={key}
                className="oui-flex oui-items-center oui-justify-start oui-gap-1"
              >
                <ChainIcon chainId={item.chain_id} size="2xs" />
                <div>{item.name}</div>
              </div>
            ))}
          </div>
        </div>
      ),
    });
  };

  return <button onClick={showDialog}>{children}</button>;
}

export const MoreIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    {...props}
  >
    <path d="M8.00521 6.66797C8.74161 6.66797 9.33854 7.26464 9.33854 8.0013C9.33854 8.73797 8.74161 9.33464 8.00521 9.33464C7.26881 9.33464 6.67188 8.73797 6.67188 8.0013C6.67188 7.26464 7.26881 6.66797 8.00521 6.66797Z" />
    <path d="M3.33333 6.66797C4.06973 6.66797 4.66667 7.26464 4.66667 8.0013C4.66667 8.73797 4.06973 9.33464 3.33333 9.33464C2.59693 9.33464 2 8.73797 2 8.0013C2 7.26464 2.59693 6.66797 3.33333 6.66797Z" />
    <path d="M12.6666 6.66797C13.403 6.66797 14 7.26464 14 8.0013C14 8.73797 13.403 9.33464 12.6666 9.33464C11.9302 9.33464 11.3333 8.73797 11.3333 8.0013C11.3333 7.26464 11.9302 6.66797 12.6666 6.66797Z" />
  </svg>
);

const EVMChains = () => {
  return (
    <div className="oui-relative oui-flex oui-items-center oui-justify-center">
      <div className="oui-flex oui-h-[18px] oui-items-center oui-justify-center ">
        <img
          src="/assets/static/sdk/portfolio/chains.png"
          className="oui-relative oui-z-0 oui-h-[18px] oui-w-[49px]"
        />
      </div>
      <div className="oui-relative oui-start-[-9px] oui-flex oui-items-center oui-justify-center oui-gap-1">
        <div className="oui-flex oui-size-[18px] oui-items-center oui-justify-center oui-rounded-full oui-bg-base-5">
          <EVMChainPopover>
            <MoreIcon
              className="oui-relative oui-z-10 oui-size-3 oui-text-base-contrast-54 hover:oui-text-base-contrast"
              style={{ zIndex: 1 }}
            />
          </EVMChainPopover>
        </div>
        <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast">
          Evm
        </div>
      </div>
    </div>
  );
};

const ShowAccount: FC = () => {
  const { state, account } = useAccount();
  const { namespace } = useWalletConnector();
  const { t } = useTranslation();
  const onCopyAddress = () => {
    navigator.clipboard.writeText(state.address ?? "");
    toast.success(t("common.copy.copied"));
  };

  const currentNamespace = useMemo(() => {
    if (namespace) {
      return namespace;
    }
    if (state.status === AccountStatusEnum.EnableTradingWithoutConnected) {
      return getLinkDeviceStorage()?.chainNamespace;
    }
    return null;
  }, [namespace, state.status]);

  const leftNode = useMemo(() => {
    if (!state.address) {
      return;
    }
    return (
      <Flex className="oui-text-base oui-text-base-contrast" gapX={2}>
        {formatAddress(state.address ?? "")}
        <button
          className="oui-cursor-pointer"
          onClick={() => {
            onCopyAddress();
          }}
        >
          <CopyIcon
            size={18}
            className="oui-text-base-contrast-80"
            opacity={1}
          />
        </button>
      </Flex>
    );
  }, [state.address]);
  const rightNode = useMemo(() => {
    if (currentNamespace === ChainNamespace.evm) {
      return <EVMChains />;
    }

    return (
      <Flex gapX={1} itemAlign={"center"} className="oui-text-2xs">
        <img
          src="/assets/static/sdk/solana-logo.png"
          className="oui-w-[15px]"
        />
        Solana
      </Flex>
    );
  }, [currentNamespace]);

  const bgClass = useMemo(() => {
    let bg = "";
    if (currentNamespace == ChainNamespace.evm) {
      bg =
        "oui-bg-[linear-gradient(15deg,rgb(var(--oui-color-primary))_-11%,transparent_30%,transparent_77%,rgb(var(--oui-color-danger-darken))_100%)]";
    } else if (currentNamespace == ChainNamespace.solana) {
      bg =
        "oui-bg-[linear-gradient(15deg,rgb(var(--oui-color-link))_-11%,transparent_30%,transparent_77%,rgb(var(--oui-color-success))_100%)]";
    }
    return bg;
  }, [currentNamespace]);
  return (
    <div
      className={cn([
        "oui-flex oui-h-[44px] oui-w-full oui-items-center oui-justify-between oui-rounded-[10px] oui-px-3 oui-py-[10px]",
        bgClass,
      ])}
    >
      {leftNode}
      {rightNode}
    </div>
  );
};

export const AccountStatusMobile: FC = () => {
  const { state } = useAccount();
  const { wrongNetwork } = useAppContext();
  const { description, rootClass, info } = useCurrentStatus();

  const alreadyShowAccount = useMemo(() => {
    if (state.status === AccountStatusEnum.EnableTradingWithoutConnected) {
      return true;
    }
    if (wrongNetwork) {
      return false;
    }
    return state.status >= AccountStatusEnum.EnableTrading;
  }, [state.status, wrongNetwork]);

  if (alreadyShowAccount) {
    return <ShowAccount />;
  }

  return (
    <div
      className={cn([
        "oui-flex oui-h-[44px] oui-w-full oui-items-center oui-justify-center oui-gap-1.5 oui-rounded-[10px] oui-px-3 oui-py-[10px]",
        rootClass,
      ])}
    >
      {info && (
        <InfoFillIcon className="oui-size-5 oui-flex-none oui-text-primary" />
      )}
      <div
        className={cn(
          "oui-flex oui-items-center oui-justify-end oui-rounded-[10px] oui-text-xs oui-font-semibold",
          info
            ? "oui-text-primary oui-font-medium"
            : "oui-text-base-contrast-54",
        )}
      >
        {description}
      </div>
    </div>
  );
};
type LinkDeviceStorage = { chainId: number; chainNamespace: ChainNamespace };

function getLinkDeviceStorage() {
  try {
    const linkDeviceStorage = localStorage.getItem("paxeerNetwork_link_device");
    const json = linkDeviceStorage ? parseJSON(linkDeviceStorage) : null;
    return json as LinkDeviceStorage;
  } catch (err) {
    console.error("getLinkDeviceStorage", err);
  }
}
