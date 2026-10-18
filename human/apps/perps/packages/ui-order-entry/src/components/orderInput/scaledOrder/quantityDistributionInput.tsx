import { FC, memo, useEffect, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { DistributionType } from "@paxeer-network/types";
import { Box, Checkbox, cn, Flex, modal, Text } from "@paxeer-network/ui";
import { useOrderEntryContext } from "../../orderEntryContext";

export type QuantityDistributionInputProps = {
  distribution_type?: DistributionType;
  className?: string;
};

export const QuantityDistributionInput: FC<QuantityDistributionInputProps> =
  memo((props) => {
    const { t } = useTranslation();
    const { setOrderValue } = useOrderEntryContext();

    const showHint = () => {
      modal.dialog({
        title: t("common.tips"),
        size: "sm",
        content: <QuantityDistributionHint value={props.distribution_type} />,
        // classNames: {
        //   content: "oui-bg-base-6",
        // },
      });
    };

    return (
      <Flex
        direction="column"
        itemAlign="start"
        justify="center"
        p={2}
        r="base"
        // gapY={1}
        width="100%"
        intensity={600}
        className={cn(
          "oui-orderEntry-quantityDistribution",
          "oui-t-rounded oui-text-base-contrast-36",
          "oui-border oui-border-solid oui-border-line",
          props.className,
        )}
      >
        <Text
          size="2xs"
          className="oui-quantityDistribution-hint oui-cursor-pointer oui-border-b oui-border-dashed oui-border-line-12"
          onClick={showHint}
        >
          {t("orderEntry.quantityDistribution")}
        </Text>
        <QuantityDistribution
          value={props.distribution_type}
          onValueChange={(value) => {
            setOrderValue("distribution_type", value);
          }}
        />
      </Flex>
    );
  });

type QuantityDistributionHintProps = {
  value?: DistributionType;
};

const QuantityDistributionHint: FC<QuantityDistributionHintProps> = (props) => {
  const { t } = useTranslation();
  const [type, setType] = useState(DistributionType.FLAT);

  useEffect(() => {
    setType(
      [
        DistributionType.FLAT,
        DistributionType.ASCENDING,
        DistributionType.DESCENDING,
      ].includes(props.value!)
        ? props.value!
        : DistributionType.FLAT,
    );
  }, [props.value]);

  const content = useMemo(() => {
    return [
      {
        type: DistributionType.FLAT,
        title: t("orderEntry.distributionType.flat"),
        description: t("orderEntry.distributionType.flat.description"),
        formula: `${t("orderEntry.skew")} = 1`,
        quantity: <FlatQuantity />,
      },
      {
        type: DistributionType.ASCENDING,
        title: t("orderEntry.distributionType.ascending"),
        description: t("orderEntry.distributionType.ascending.description"),
        formula: `${t("orderEntry.skew")} > 1`,
        quantity: <AscendingQuantity />,
      },
      {
        type: DistributionType.DESCENDING,
        title: t("orderEntry.distributionType.descending"),
        description: t("orderEntry.distributionType.descending.description"),
        formula: `0 < ${t("orderEntry.skew")} < 1`,
        quantity: <DescendingQuantity />,
      },
    ];
  }, [t]);

  const currentContent = useMemo(() => {
    return content.find((item) => item.type === type);
  }, [content, type]);

  return (
    <div className="oui-text-2xs oui-font-semibold oui-text-base-contrast-54">
      <Flex direction="column" itemAlign="start" gapY={1}>
        <Text intensity={80}>
          {t("orderEntry.quantityDistribution.description")}
        </Text>
        <Text>{t("orderEntry.quantityDistribution.formula")}</Text>
      </Flex>
      <Flex intensity={600} p={1} r="base" mt={3} itemAlign="start">
        <Flex direction="column" itemAlign="start">
          {content.map((item) => (
            <Box
              key={item.title}
              intensity={type === item.type ? 500 : 600}
              width={78}
              p={2}
              className="oui-cursor-pointer oui-rounded-s"
              onClick={() => {
                setType(item.type);
              }}
            >
              <Text>{item.title}</Text>
            </Box>
          ))}
        </Flex>
        <Flex
          direction="column"
          itemAlign="start"
          gapY={1}
          p={2}
          intensity={500}
          width="100%"
          className={cn(
            "oui-rounded-e",
            type === DistributionType.FLAT && "oui-rounded-es",
            type === DistributionType.ASCENDING && "oui-rounded-s",
            type === DistributionType.DESCENDING && "oui-rounded-ss",
          )}
        >
          <div>{currentContent?.formula}</div>
          <Text>{currentContent?.description}</Text>
          <Flex width="100%" justify="center">
            {currentContent?.quantity}
          </Flex>
          <Flex width="100%" gapX={1} justify="center">
            <Text>{t("common.price")}</Text>
            <PriceChart />
          </Flex>
        </Flex>
      </Flex>
    </div>
  );
};

type QuantityDistributionProps = {
  value?: DistributionType;
  onValueChange: (value: DistributionType) => void;
};

const QuantityDistribution: FC<QuantityDistributionProps> = (props) => {
  const { value, onValueChange } = props;
  const { t } = useTranslation();

  const onChange = (value: DistributionType) => (checked: boolean) => {
    onValueChange(value);
  };

  const distributionTypeMap = useMemo(() => {
    return {
      [DistributionType.FLAT]: t("orderEntry.distributionType.flat"),
      [DistributionType.ASCENDING]: t(
        "orderEntry.distributionType.ascending.abbr",
      ),
      [DistributionType.DESCENDING]: t(
        "orderEntry.distributionType.descending.abbr",
      ),
      [DistributionType.CUSTOM]: t("orderEntry.distributionType.custom"),
    };
  }, [t]);

  return (
    <Flex
      className={cn(
        "oui-orderEntry-quantityDistribution-options",
        "oui-gap-x-[6px] lg:oui-gap-x-2",
      )}
      wrap="wrap"
    >
      {Object.values(DistributionType).map((type) => {
        return (
          <Flex itemAlign={"center"} key={type}>
            <Checkbox
              id={`distribution-type-${type}`}
              color={"white"}
              variant={"radio"}
              className="oui-quantityDistribution-option-radio"
              checked={value === type}
              onCheckedChange={onChange(type)}
            />
            <label
              htmlFor={`distribution-type-${type}`}
              className={cn(
                "oui-quantityDistribution-option-label",
                "oui-text-2xs",
                "oui-ms-1",
                "oui-whitespace-nowrap oui-break-normal",
              )}
            >
              {distributionTypeMap[type]}
            </label>
          </Flex>
        );
      })}
    </Flex>
  );
};

// Apply opacity to each SVG as a group to avoid double blending at overlaps.
const quantityHintFill = "rgb(var(--oui-color-base-foreground))";

const FlatQuantity = () => {
  return (
    <svg
      width="218"
      height="40"
      viewBox="0 0 218 40"
      fill="none"
      opacity={0.54}
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect x="18" y="18" width="181" height="4" fill={quantityHintFill} />
      <circle
        cx="15"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="62"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="109"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="156"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="203"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
    </svg>
  );
};

const AscendingQuantity = () => {
  return (
    <svg
      width="218"
      height="40"
      viewBox="0 0 218 40"
      fill="none"
      opacity={0.54}
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect x="18" y="18" width="181" height="4" fill={quantityHintFill} />
      <circle
        cx="15"
        cy="20"
        r="4.28571"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="1.42857"
      />
      <circle
        cx="62"
        cy="20"
        r="5.14286"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="1.71429"
      />
      <circle
        cx="109"
        cy="20"
        r="6"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="156"
        cy="20"
        r="6.85714"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2.28571"
      />
      <circle
        cx="203"
        cy="20"
        r="7.71429"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2.57143"
      />
    </svg>
  );
};

const DescendingQuantity = () => {
  return (
    <svg
      width="218"
      height="40"
      viewBox="0 0 218 40"
      fill="none"
      opacity={0.54}
      xmlns="http://www.w3.org/2000/svg"
    >
      <rect
        width="181"
        height="4"
        transform="matrix(-1 0 0 1 200 18)"
        fill={quantityHintFill}
      />
      <circle
        cx="5"
        cy="5"
        r="4.28571"
        transform="matrix(-1 0 0 1 208 15)"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="1.42857"
      />
      <circle
        cx="6"
        cy="6"
        r="5.14286"
        transform="matrix(-1 0 0 1 162 14)"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="1.71429"
      />
      <circle
        cx="7"
        cy="7"
        r="6"
        transform="matrix(-1 0 0 1 116 13)"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2"
      />
      <circle
        cx="8"
        cy="8"
        r="6.85714"
        transform="matrix(-1 0 0 1 70 12)"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2.28571"
      />
      <circle
        cx="9"
        cy="9"
        r="7.71429"
        transform="matrix(-1 0 0 1 24 11)"
        fill={quantityHintFill}
        stroke="rgb(var(--oui-color-base-5))"
        strokeWidth="2.57143"
      />
    </svg>
  );
};

const PriceChart = () => {
  return (
    <svg
      width="176"
      height="8"
      viewBox="0 0 176 8"
      fill="none"
      opacity={0.54}
      xmlns="http://www.w3.org/2000/svg"
    >
      <path
        d="M175.354 4.35355C175.549 4.15829 175.549 3.84171 175.354 3.64645L172.172 0.464466C171.976 0.269204 171.66 0.269204 171.464 0.464466C171.269 0.659728 171.269 0.976311 171.464 1.17157L174.293 4L171.464 6.82843C171.269 7.02369 171.269 7.34027 171.464 7.53553C171.66 7.7308 171.976 7.7308 172.172 7.53553L175.354 4.35355ZM0 4V4.5H175V4V3.5H0V4Z"
        fill={quantityHintFill}
      />
    </svg>
  );
};
