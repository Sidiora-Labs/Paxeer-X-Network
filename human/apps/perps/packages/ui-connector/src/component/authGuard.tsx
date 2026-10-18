import React, { ReactElement, useCallback, useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import {
  AccountStatusEnum,
  isWalletChainChangePendingResult,
  NetworkId,
} from "@paxeer-network/types";
import {
  Button,
  Either,
  modal,
  Text,
  toast,
  useScreen,
  type ButtonProps,
} from "@paxeer-network/ui";
import { Flex } from "@paxeer-network/ui";
import { Box } from "@paxeer-network/ui";
import {
  ChainSelectorDialogId,
  ChainSelectorSheetId,
} from "@paxeer-network/ui-chain-selector";
import { useChainChangeValidation } from "../hooks/useChainChangeValidation";
import { useOnboardingModal } from "../hooks/useOnboardingModal";

type ChainProps = {
  networkId?: NetworkId;
  bridgeLessOnly?: boolean;
};

export type alertMessages = {
  connectWallet?: string;
  switchChain?: string;
  enableTrading?: string;
  signin?: string;
};

export type AuthGuardProps = React.ButtonHTMLAttributes<HTMLButtonElement> & {
  fallback?: (props: {
    validating: boolean;
    status: AccountStatusEnum;
    wrongNetwork: boolean;
  }) => ReactElement;
  // indicator?: ReactElement;
  /**
   * Required state to be satisfied
   * @default AccountStatusEnum.EnableTrading
   */
  status?: AccountStatusEnum;

  bridgeLessOnly?: boolean;

  buttonProps?: ButtonProps;

  descriptions?: alertMessages;

  labels?: alertMessages;

  classNames?: {
    root?: string;
    description?: string;
    // button?: string;
  };

  networkId?: NetworkId;

  // validatingIndicator?: ReactElement;
};

const useConnectWalletHandler = (options: {
  networkId?: NetworkId;
  bridgeLessOnly?: boolean;
}) => {
  const { networkId, bridgeLessOnly } = options;
  const { t } = useTranslation();
  const { connectWallet } = useAppContext();
  const { state } = useAccount();
  const { isMobile } = useScreen();
  const { openOnboardingModal, handleAccountStatus } = useOnboardingModal();

  const onConnectPaxeerNetwork = useCallback(() => {
    void openOnboardingModal(state.status);
  }, [openOnboardingModal, state.status]);

  const handleValidatedStatus = useCallback(
    (status: AccountStatusEnum) => {
      if (status < AccountStatusEnum.EnableTrading) {
        handleAccountStatus(status);
      } else {
        toast.success(t("connector.walletConnected"));
      }
    },
    [handleAccountStatus, t],
  );
  const { onChainChangeBefore, onChainChangeAfter } = useChainChangeValidation({
    onAccountValidated: handleValidatedStatus,
  });

  const switchChain = useCallback(() => {
    modal
      .show<{ wrongNetwork: boolean }>(
        isMobile ? ChainSelectorSheetId : ChainSelectorDialogId,
        {
          networkId,
          bridgeLessOnly,
          onChainChangeBefore,
          onChainChangeAfter,
        },
      )
      .catch((error) => {
        if (isWalletChainChangePendingResult(error)) {
          return;
        }
        console.error("[switchChain error]", error);
      });
  }, [
    bridgeLessOnly,
    isMobile,
    networkId,
    onChainChangeAfter,
    onChainChangeBefore,
  ]);

  const onConnectWallet = useCallback(async () => {
    try {
      const res = await connectWallet();

      if (!res) {
        return;
      }

      if (res.wrongNetwork) {
        switchChain();
      } else {
        handleAccountStatus(res.status);
      }
    } catch (error) {
      console.error(error);
    }
  }, [connectWallet, handleAccountStatus, switchChain]);

  return { onConnectPaxeerNetwork, onConnectWallet, switchChain };
};

export const AuthGuard: React.FC<React.PropsWithChildren<AuthGuardProps>> = (
  props,
) => {
  const {
    status,
    buttonProps,
    fallback,
    descriptions,
    classNames,
    networkId,
    id,
    bridgeLessOnly,
    // ...rest
  } = props;
  const { t } = useTranslation();
  const { state } = useAccount();
  const { wrongNetwork, disabledConnect } = useAppContext();
  const { onConnectPaxeerNetwork, onConnectWallet, switchChain } =
    useConnectWalletHandler({ networkId, bridgeLessOnly });

  const _status = useMemo(() => {
    if (status === undefined) {
      return state.status === AccountStatusEnum.EnableTradingWithoutConnected
        ? AccountStatusEnum.EnableTradingWithoutConnected
        : AccountStatusEnum.EnableTrading;
    }
    return status;
  }, [status, state.status]);

  const labels = {
    connectWallet: t("connector.connectWallet"),
    switchChain: t("connector.wrongNetwork"),
    enableTrading: t("connector.enableTrading"),
    signin: t("connector.createAccount"),
    ...props.labels,
  };

  // return Match(state.status)
  //   .with(AccountStatusEnum.EnableTrading, () => props.children)
  //   .with(AccountStatusEnum.DisableTrading, () => props.fallback)
  //   .with(AccountStatusEnum.Validating, () => props.validatingIndicator)
  //   .otherwise(() => props.fallback);
  //

  const Left = useMemo<ReactElement>(() => {
    if (typeof fallback !== "undefined") {
      return fallback({
        validating: state.validating,
        status: state.status,
        wrongNetwork,
      });
    }

    if (state.validating && !disabledConnect) {
      return (
        <StatusInfo
          // fullWidth
          disabled
          loading
          description={descriptions?.connectWallet}
          id={id}
          type="button"
          {...buttonProps}
        >
          {labels.connectWallet}
        </StatusInfo>
      );
    }

    return (
      <DefaultFallback
        bridgeLessOnly={bridgeLessOnly}
        status={state.status}
        buttonProps={{ ...buttonProps, id, type: "button" }}
        wrongNetwork={wrongNetwork}
        networkId={props.networkId}
        labels={labels}
        descriptions={descriptions}
        disabledConnect={disabledConnect}
        onConnectPaxeerNetwork={onConnectPaxeerNetwork}
        onConnectWallet={onConnectWallet}
        switchChain={switchChain}
      />
    );
  }, [
    state.status,
    state.validating,
    buttonProps,
    wrongNetwork,
    labels,
    descriptions,
    onConnectPaxeerNetwork,
    onConnectWallet,
    switchChain,
  ]);

  /**
   * **Important: The chldren component will be rendered only if the status is equal to the required status and the network is correct.**
   */

  return (
    <Either
      value={state.status >= _status && !wrongNetwork && !disabledConnect}
      left={Left}
    >
      {props.children}
    </Either>
  );
};

const DefaultFallback: React.FC<{
  status: AccountStatusEnum;
  wrongNetwork: boolean;
  buttonProps?: ButtonProps;
  networkId?: NetworkId;
  labels: alertMessages;
  bridgeLessOnly?: boolean;
  descriptions?: alertMessages;
  disabledConnect?: boolean;
  onConnectPaxeerNetwork: () => void;
  onConnectWallet: () => Promise<void>;
  switchChain: () => void;
}> = (props) => {
  const { buttonProps, labels, descriptions } = props;
  const { onConnectPaxeerNetwork, onConnectWallet, switchChain } = props;

  if (props.wrongNetwork && !props.disabledConnect) {
    return (
      <StatusInfo
        color="warning"
        // size="md"
        // fullWidth
        onClick={() => {
          switchChain();
        }}
        description={descriptions?.switchChain}
        {...buttonProps}
      >
        {labels.switchChain}
      </StatusInfo>
    );
  }

  if (props.status <= AccountStatusEnum.NotConnected || props.disabledConnect) {
    return (
      <StatusInfo
        size="lg"
        onClick={() => {
          onConnectWallet();
        }}
        // fullWidth
        description={descriptions?.connectWallet}
        disabled={props.disabledConnect}
        {...buttonProps}
      >
        {labels.connectWallet}
      </StatusInfo>
    );
  }

  if (props.status <= AccountStatusEnum.NotSignedIn) {
    return (
      <StatusInfo
        size="lg"
        onClick={() => {
          onConnectPaxeerNetwork();
        }}
        // fullWidth
        description={descriptions?.signin}
        {...buttonProps}
      >
        {labels.signin}
      </StatusInfo>
    );
  }

  return (
    <StatusInfo
      size="lg"
      // fullWidth
      description={descriptions?.enableTrading}
      {...buttonProps}
      onClick={() => onConnectPaxeerNetwork()}
    >
      {labels.enableTrading}
    </StatusInfo>
  );

  // return (
  //   <Match
  //     value={props.status}
  //     case={(value: AccountStatusEnum) => {
  //       if (value <= AccountStatusEnum.NotConnected || props.disabledConnect) {
  //         return (
  //           <StatusInfo
  //             size="lg"
  //             onClick={() => {
  //               onConnectWallet();
  //             }}
  //             // fullWidth
  //             variant={props.disabledConnect ? undefined : "gradient"}
  //             angle={45}
  //             description={descriptions?.connectWallet}
  //             disabled={props.disabledConnect}
  //             {...buttonProps}
  //           >
  //             {labels.connectWallet}
  //           </StatusInfo>
  //         );
  //       }
  //       if (value <= AccountStatusEnum.NotSignedIn) {
  //         return (
  //           <StatusInfo
  //             size="lg"
  //             onClick={() => {
  //               onConnectPaxeerNetwork();
  //             }}
  //             // fullWidth
  //             angle={45}
  //             description={descriptions?.signin}
  //             {...buttonProps}
  //           >
  //             {labels.signin}
  //           </StatusInfo>
  //         );
  //       }
  //     }}
  //     default={
  //       <StatusInfo
  //         size="lg"
  //         // fullWidth
  //         description={descriptions?.enableTrading}
  //         {...buttonProps}
  //         onClick={() => onConnectPaxeerNetwork()}
  //       >
  //         {labels.enableTrading}
  //       </StatusInfo>
  //     }
  //   />
  // );
};

AuthGuard.displayName = "AuthGuard";

const StatusInfo: React.FC<ButtonProps & { description?: string }> = (
  props,
) => {
  const { description, ...buttonProps } = props;
  return (
    <Flex direction={"column"}>
      <Button {...buttonProps}></Button>
      {!!description && (
        <Box mt={4} className="oui-leading-none" style={{ lineHeight: 0 }}>
          <Text size="2xs" intensity={36}>
            {description}
          </Text>
        </Box>
      )}
    </Flex>
  );
};
