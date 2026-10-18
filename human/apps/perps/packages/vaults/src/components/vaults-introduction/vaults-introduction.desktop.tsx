import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Text, Divider, cn, TextProps, useScreen } from "@paxeer-network/ui";
import { VaultsIntroductionScript } from "./vaults-introduction.script";

export const VaultsIntroductionDesktop: FC<VaultsIntroductionScript> = (
  props,
) => {
  const { t } = useTranslation();
  const { isMobile } = useScreen();
  const { vaultsInfo } = props;

  return (
    <div
      className={cn(
        "oui-flex oui-items-center",
        "oui-bg-primary-darken",
        isMobile
          ? "oui-gap-3 oui-rounded-xl oui-p-3"
          : "oui-gap-6 oui-rounded-2xl oui-p-6",
      )}
    >
      <VaultsIntroductionItem
        title={t("vaults.introduction.tvl")}
        value={vaultsInfo.tvl}
        textProps={{ currency: "$" } as any}
        isMobile={isMobile}
      />
      <Divider
        direction="vertical"
        intensity={12}
        className={isMobile ? "oui-h-[44px]" : "oui-h-[64px]"}
      />
      <VaultsIntroductionItem
        title={t("common.vaults")}
        value={vaultsInfo.vaultsCount}
        isMobile={isMobile}
      />
      <Divider
        direction="vertical"
        intensity={12}
        className={isMobile ? "oui-h-[44px]" : "oui-h-[64px]"}
      />
      <VaultsIntroductionItem
        title={t("vaults.introduction.depositors")}
        value={vaultsInfo.lpCount}
        isMobile={isMobile}
      />
    </div>
  );
};

const VaultsIntroductionItem: FC<{
  title: string;
  value: number;
  textProps?: TextProps;
  isMobile?: boolean;
}> = (props) => {
  const { title, value, textProps, isMobile } = props;

  return (
    <div
      className={cn(
        "oui-flex oui-flex-1 oui-flex-col oui-items-center ",
        isMobile ? "oui-gap-0.5" : "oui-gap-3",
      )}
    >
      <div
        className={cn(
          "oui-font-normal oui-text-primary-contrast",
          isMobile ? "oui-text-2xs" : "oui-text-base",
        )}
      >
        {title}
      </div>
      <Text.numeral
        className={cn(
          "oui-font-semibold oui-text-primary-contrast",
          isMobile
            ? "oui-text-base"
            : "oui-text-[length:clamp(28px,2.2vw,38px)] oui-tracking-[-0.02em] oui-leading-none",
        )}
        dp={0}
        {...textProps}
      >
        {value}
      </Text.numeral>
    </div>
  );
};
