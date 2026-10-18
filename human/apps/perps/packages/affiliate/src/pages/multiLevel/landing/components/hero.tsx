import { useMemo } from "react";
import { useAccount } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { useAppContext } from "@paxeer-network/react-app";
import { AccountStatusEnum } from "@paxeer-network/types";
import { Button, cn, Flex, modal, Text } from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import { useReferralContext } from "../../../../provider";
import { ReferralCodeFormType } from "../../../../types";
import { ReferralCodeFormDialogId } from "../../affiliate/referralCodeForm/modal";
import type { BindReferralCodeSuccessPayload } from "../../components/bindReferralCode/bindReferralCode.widget";
import { BindReferralCodeDialogId } from "../../components/bindReferralCode/modal";
import { TradingVolumeProgress } from "../../components/tradingVolumeProgress";
import { NetworkDiagram } from "./networkDiagram";

/** SWR mutate may resolve to various shapes — only treat object rebate configs as valid. */
function parseMaxRebateRateInfoFromSettled(
  result: PromiseSettledResult<unknown>,
):
  | {
      max_rebate_rate?: number;
      bonus_max_rebate_rate?: number;
      base_rebate_rate?: number;
    }
  | undefined {
  if (result.status !== "fulfilled") return undefined;
  if (typeof result.value !== "object" || result.value === null) {
    return undefined;
  }
  return result.value as {
    max_rebate_rate?: number;
    bonus_max_rebate_rate?: number;
    base_rebate_rate?: number;
  };
}

export const Hero = () => {
  const { t } = useTranslation();
  const { state } = useAccount();
  const { wrongNetwork } = useAppContext();
  const status = state.status;

  const {
    isMultiLevelReferralUnlocked,
    isMultiLevelEnabled,
    isTrader,
    multiLevelRebateInfo,
    multiLevelRebateInfoMutate,
    maxRebateRate,
    maxRebateRateInfo,
    bonusMaxRebateRate,
    baseRebateRate,
    maxRebateRateMutate,
    mutate,
    referralInfo,
    generateCode,
  } = useReferralContext();

  const boundReferralCode =
    referralInfo?.referee_info?.referer_code?.trim() ?? "";

  const openBindReferralOnlyModal = () => {
    modal.show(BindReferralCodeDialogId, {
      onSuccess: async ({ skipped }: BindReferralCodeSuccessPayload) => {
        if (skipped) return;
        await Promise.allSettled([multiLevelRebateInfoMutate?.(), mutate?.()]);
      },
    });
  };

  const showCreateReferralCodeModal = (maxRateInfoOverride?: {
    max_rebate_rate?: number;
    bonus_max_rebate_rate?: number;
    base_rebate_rate?: number;
  }) => {
    const latestMaxRateInfo = maxRateInfoOverride ?? maxRebateRateInfo;
    modal.show(ReferralCodeFormDialogId, {
      type: ReferralCodeFormType.Create,
      maxRebateRate: latestMaxRateInfo?.max_rebate_rate ?? maxRebateRate ?? 0,
      bonusMaxRebateRate:
        latestMaxRateInfo?.bonus_max_rebate_rate ?? bonusMaxRebateRate,
      baseRebateRate: latestMaxRateInfo?.base_rebate_rate ?? baseRebateRate,
      referralCode: generateCode?.code,
      directBonusRebateRate: 0,
      onSuccess: () => {
        multiLevelRebateInfoMutate();
      },
    });
  };

  const onCreateReferralCode = () => {
    // if not bound to any codes, show the bind modal
    if (!isTrader) {
      modal.show(BindReferralCodeDialogId, {
        onSuccess: async ({ skipped }: BindReferralCodeSuccessPayload) => {
          if (skipped) {
            showCreateReferralCodeModal();
            return;
          }
          const results = await Promise.allSettled([
            maxRebateRateMutate(),
            multiLevelRebateInfoMutate(),
            mutate(),
          ]);

          const latestMaxRebateRateInfo = parseMaxRebateRateInfoFromSettled(
            results[0],
          );

          showCreateReferralCodeModal(latestMaxRebateRateInfo);
        },
      });
      return;
    }

    showCreateReferralCodeModal();
  };

  const description = useMemo(() => {
    if (wrongNetwork) {
      return t("affiliate.wrongNetwork.description");
    }

    if (status === AccountStatusEnum.NotConnected) {
      return t("affiliate.newReferralProgram.description");
    }

    if (
      status > AccountStatusEnum.Connected &&
      status < AccountStatusEnum.EnableTrading
    ) {
      return t("affiliate.setUpAccount.description");
    }

    if (!isMultiLevelReferralUnlocked) {
      return "";
    }

    return t("affiliate.newReferralProgram.description");
  }, [t, wrongNetwork, status, isMultiLevelReferralUnlocked]);

  const renderContent = () => {
    if (!isMultiLevelReferralUnlocked) {
      return (
        <Flex
          direction="column"
          itemAlign="start"
          gap={4}
          className="oui-w-full"
        >
          <TradingVolumeProgress
            classNames={{
              root: "oui-items-start",
              description: "!oui-text-start",
            }}
            buttonProps={{
              size: "xl",
            }}
          />
          {boundReferralCode ? (
            <Text size="sm" intensity={54} className="oui-leading-normal">
              {t("affiliate.newReferralProgram.referredBy", {
                code: boundReferralCode,
              })}
            </Text>
          ) : (
            <button
              type="button"
              className={cn(
                "oui-cursor-pointer oui-border-none oui-bg-transparent oui-p-0",
                "oui-text-start oui-text-sm oui-font-normal oui-text-base-contrast-54 oui-underline oui-underline-offset-2",
                "oui-affiliate-landing-hero-bindReferralCode",
              )}
              onClick={openBindReferralOnlyModal}
            >
              {t("affiliate.newReferralProgram.wereYouReferred")}
            </button>
          )}
        </Flex>
      );
    }

    if (isMultiLevelEnabled && !multiLevelRebateInfo?.referral_code) {
      return (
        <Flex
          direction="column"
          itemAlign="start"
          gap={4}
          className="oui-w-full"
        >
          <Button size="lg" className="oui-px-4" onClick={onCreateReferralCode}>
            {t("affiliate.referralCode.create")}
          </Button>
          {boundReferralCode ? (
            <Text size="sm" intensity={54} className="oui-leading-normal">
              {t("affiliate.newReferralProgram.referredBy", {
                code: boundReferralCode,
              })}
            </Text>
          ) : null}
        </Flex>
      );
    }

    return "";

    // return (
    //   <Button size="lg" className="oui-px-4" disabled>
    //     {t("affiliate.accountNotEligible")}
    //   </Button>
    // );
  };

  return (
    <Flex
      gap={8}
      className="oui-affiliate-hero oui-mb-[clamp(2rem,3vw,3rem)] oui-flex-col-reverse md:oui-flex-row"
      id="oui-affiliate-landing-hero"
      itemAlign={"center"}
    >
      <Flex direction="column" itemAlign="start" gap={6} className="oui-flex-1">
        <Flex
          direction="column"
          gap={3}
          itemAlign="start"
          id="oui-affiliate-landing-hero-title"
        >
          <Text
            weight="semibold"
            className="oui-text-[32px] oui-leading-tight md:oui-text-[48px]"
          >
            {t("affiliate.landing.title")}
          </Text>
        </Flex>

        {description && (
          <Text size="sm" intensity={54}>
            {description}
          </Text>
        )}

        <AuthGuard
          labels={{
            connectWallet: t("affiliate.connectWallet"),
            signin: t("affiliate.setUpAccount"),
            enableTrading: t("affiliate.setUpAccount"),
          }}
        >
          {renderContent()}
        </AuthGuard>
      </Flex>

      <NetworkDiagram
        role="img"
        aria-label="Network diagram"
        className="oui-size-[335px] md:oui-size-[480px]"
        id="oui-affiliate-landing-network-diagram"
      />
    </Flex>
  );
};
