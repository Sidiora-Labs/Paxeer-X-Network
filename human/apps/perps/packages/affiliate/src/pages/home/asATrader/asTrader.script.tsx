import { useMemo, useState } from "react";
import {
  isReferralCodeLengthValid,
  REFERRAL_CODE_MIN_LENGTH,
  useAccount,
  useCheckReferralCode,
  useMutation,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { toast, useScreen } from "@paxeer-network/ui";
import { AuthStatusEnum, useAuthStatus } from "@paxeer-network/ui-connector";
import { TabTypes, useReferralContext } from "../../../provider";

export const useAsTraderScript = () => {
  const { t } = useTranslation();

  const {
    isTrader,
    referralInfo,
    setShowHome,
    bindReferralCodeState,
    setTab,
    mutate,
  } = useReferralContext();

  const { wrongNetwork, disabledConnect } = useAppContext();

  const { state } = useAccount();

  const onEnterTraderPage = () => {
    setTab(TabTypes.trader);
    setShowHome(false);
  };

  const [code, setCode] = useState("");
  const [open, setOpen] = useState(false);

  const {
    isExist,
    error: checkCodeError,
    isLoading,
  } = useCheckReferralCode(
    code.length >= REFERRAL_CODE_MIN_LENGTH ? code : undefined,
  );

  const hide = () => {
    setOpen(false);
  };

  const [bindCode, { error, isMutating }] = useMutation(
    "/v1/referral/bind",
    "POST",
  );

  const onClickConfirm = async () => {
    if (!isReferralCodeLengthValid(code) || !isExist) {
      return;
    }

    try {
      await bindCode({ referral_code: code });
      toast.success(t("affiliate.referralCode.bound"));
      mutate();
      if (bindReferralCodeState) {
        bindReferralCodeState(true, null, hide, { tab: 1 });
      } else {
        hide();
      }
    } catch (e: any) {
      let errorText = `${e}`;
      if ("message" in e) {
        errorText = e.message;
      }

      if ("referral code not exist" === errorText) {
        errorText = t("affiliate.referralCode.notExist");
      }

      if (bindReferralCodeState) {
        toast.error(errorText);
        bindReferralCodeState(false, e, hide, {});
      } else {
        toast.error(errorText);
      }
    }
  };

  const { isMobile } = useScreen();

  const authStatus = useAuthStatus();

  const warningMessage = useMemo(() => {
    const message: { [key in AuthStatusEnum]?: string } = {
      [AuthStatusEnum.ConnectWallet]: t("affiliate.connectWallet.tooltip"),
      [AuthStatusEnum.CreateAccount]: t("affiliate.createAccount.tooltip"),
      [AuthStatusEnum.WrongNetwork]: t("connector.wrongNetwork.tooltip"),
    };
    return message[authStatus];
  }, [t, authStatus]);

  return {
    isTrader,
    isLoading,
    referralInfo,
    onEnterTraderPage,
    code,
    setCode,
    open,
    setOpen,
    onClickConfirm,
    isExist,
    wrongNetwork,
    isMobile,
    warningMessage,
  };
};

export type AsTraderReturns = ReturnType<typeof useAsTraderScript>;
