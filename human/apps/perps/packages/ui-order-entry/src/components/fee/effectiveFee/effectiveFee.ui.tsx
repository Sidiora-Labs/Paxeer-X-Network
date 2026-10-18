import React from "react";
import { useFeeState } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Flex,
  modal,
  Text,
  Tooltip,
  useModal,
  useScreen,
} from "@paxeer-network/ui";
import { AuthGuard } from "@paxeer-network/ui-connector";
import {
  RouterAdapter,
  useScaffoldContext,
} from "@paxeer-network/ui-scaffold";
import { EffectiveFee } from "./icons";

const EffectiveFeeBody: React.FC<{
  routerAdapter: RouterAdapter | undefined;
  onClose?: () => void;
}> = ({ routerAdapter, onClose }) => {
  const { t } = useTranslation();
  return (
    <Text size="2xs" className="oui-whitespace-normal oui-break-words">
      {t("portfolio.feeTier.effectiveFee.tooltip")}{" "}
      <a
        href="/rewards/affiliate"
        onClick={(e) => {
          e.preventDefault();
          routerAdapter?.onRouteChange({
            href: "/rewards/affiliate",
            name: t("portfolio.feeTier.effectiveFee.tooltipLink"),
          });
          onClose?.();
        }}
        className="oui-cursor-pointer oui-border-none oui-bg-transparent oui-p-0 oui-text-2xs oui-underline hover:oui-text-base-contrast-80"
      >
        {t("portfolio.feeTier.effectiveFee.tooltipLink")}
      </a>
    </Text>
  );
};

const EffectiveFeeMobileContent: React.FC<{
  routerAdapter: RouterAdapter | undefined;
}> = ({ routerAdapter }) => {
  const { hide } = useModal();
  return <EffectiveFeeBody routerAdapter={routerAdapter} onClose={hide} />;
};

const EffectiveFeeSection: React.FC<{
  routerAdapter: RouterAdapter | undefined;
}> = (props) => {
  const { routerAdapter } = props;
  const { isMobile } = useScreen();
  const { t } = useTranslation();
  if (isMobile) {
    return (
      <EffectiveFee
        onClick={() => {
          modal.dialog({
            size: "sm",
            title: t("common.tips"),
            content: (
              <EffectiveFeeMobileContent routerAdapter={routerAdapter} />
            ),
          });
        }}
      />
    );
  }
  return (
    <Tooltip
      content={<EffectiveFeeBody routerAdapter={routerAdapter} />}
      className="oui-p-1.5 oui-text-base-contrast-54"
    >
      <EffectiveFee className={"oui-cursor-pointer"} />
    </Tooltip>
  );
};

export const EffectiveFeeUI: React.FC<{ taker: string; maker: string }> = (
  props,
) => {
  const { t } = useTranslation();
  const { routerAdapter } = useScaffoldContext();
  const { taker, maker } = props;

  const originalTrailingFees = (
    <Flex
      itemAlign="center"
      justify="between"
      width={"100%"}
      gap={1}
      className="oui-orderEntry-effectiveFees"
    >
      <Flex width={"100%"} itemAlign="center" justify={"between"}>
        <Text className="oui-truncate" size="2xs">
          {t("common.fees")}
        </Text>
        <AuthGuard
          fallback={() => (
            <Text className="oui-truncate" size="2xs">
              {t("dmm.taker")}: --% / {t("dmm.maker")}: --%
            </Text>
          )}
        >
          <Flex gap={1}>
            <Text className="oui-truncate" size="2xs">
              {t("dmm.taker")}:
            </Text>
            <Text size="2xs" className="oui-text-base-contrast-80">
              {taker}
            </Text>
            <Text size="2xs">/</Text>
            <Text className="oui-truncate" size="2xs">
              {t("dmm.maker")}:
            </Text>
            <Text size="2xs" className="oui-text-base-contrast-80">
              {maker}
            </Text>
          </Flex>
        </AuthGuard>
      </Flex>
      <EffectiveFeeSection routerAdapter={routerAdapter} />
    </Flex>
  );

  return originalTrailingFees;
};
