import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  CaretDownIcon,
  Flex,
  Text,
  TokenIcon,
  Tips,
} from "@paxeer-network/ui";

type DepositTokenValueFormatterProps = {
  value: string;
  userMaxQty?: number | null;
};

export const DepositTokenValueFormatter: FC<
  DepositTokenValueFormatterProps
> = ({ value, userMaxQty }) => {
  const { t } = useTranslation();

  const tipContent = (
    <Flex direction="column" itemAlign="start">
      <Text size="2xs" weight="semibold" intensity={36}>
        {t("transfer.depositCap.tooltip")}
        <Text as="span" size="2xs" weight="semibold" intensity={80}>
          {value}.
        </Text>
      </Text>
      <a
        href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/multi-collateral#max-deposits-user"
        target="_blank"
        rel="noopener noreferrer"
        className="oui-text-2xs oui-text-primary"
      >
        {t("common.learnMore")}
      </a>
    </Flex>
  );

  return (
    <Flex direction="column" itemAlign="end" gapY={1}>
      <Flex gapX={1} itemAlign="center">
        <TokenIcon name={value} className="oui-size-[16px]" />
        <Text weight="semibold" intensity={54}>
          {value}
        </Text>
        <CaretDownIcon
          size={12}
          className="oui-text-base-contrast-54"
          opacity={1}
        />
      </Flex>
      <Flex itemAlign="center" className="oui-gap-[2px]">
        <Text
          size="2xs"
          intensity={36}
          weight="regular"
          className="oui-leading-[10px]"
        >
          {t("transfer.depositCap", "Deposit cap")}{" "}
          <Text.numeral
            as="span"
            size="2xs"
            intensity={80}
            weight="regular"
            className="oui-leading-[10px]"
            dp={0}
          >
            {userMaxQty ?? 0}
          </Text.numeral>
        </Text>
        <Tips content={tipContent} title={t("common.tips")} />
      </Flex>
    </Flex>
  );
};
