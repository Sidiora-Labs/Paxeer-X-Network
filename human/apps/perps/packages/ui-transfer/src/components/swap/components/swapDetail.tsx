import { FC, ReactNode } from "react";
import { useTranslation, Trans } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Box,
  Flex,
  Text,
  TokenIcon,
  Icon,
  Divider,
  Button,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { feeDecimalsOffset } from "../../../utils";
import { ArrowRightIcon, GasFeeIcon, WoofiIcon } from "../icons";
import { SwapMode, SymbolInfo } from "../types";

export interface SwapInfo {
  // gasFee: string;
  // tradingFee: string;
  dstGasFee: string;
  swapFee: string;
  bridgeFee: string;
  price: number;
  slippage: number;
  received: string;
  time: number;
}

type SwapDetailProps = {
  onConfirm: () => void;
  mode: SwapMode;
  src: SymbolInfo;
  dst: SymbolInfo;
  info: SwapInfo;
  markPrice: number;
  nativePrice: number;
  nativeToken?: API.TokenInfo;
  viewMode: "processing" | "details";
  // transactionData: any;
};

export const SwapDetail: FC<SwapDetailProps> = (props) => {
  const {
    info,
    mode,
    nativePrice,
    markPrice,
    src,
    dst,
    nativeToken,
    viewMode,
  } = props;

  const { t } = useTranslation();

  const header = (
    <Flex justify="between">
      <SwapSymbol
        primary={src?.token}
        chainId={src.chain}
        amount={src?.amount}
        dp={src?.displayDecimals}
        symbol={src?.token}
      />
      <ArrowRightIcon className="oui-text-primary-light" />
      <SwapSymbol
        primary={dst?.token}
        component={<WoofiIcon className="oui-size-4" />}
        amount={dst?.amount}
        dp={dst?.displayDecimals}
        symbol={dst?.token}
        reverse
      />
    </Flex>
  );

  const divider = (
    <Box my={5}>
      <Divider intensity={12} />
    </Box>
  );

  const listData = [
    {
      label: t("transfer.deposit.destinationGasFee"),
      value: (
        <Flex gapX={1}>
          <GasFeeIcon className="oui-text-primary-light" />
          <Text.numeral
            color="primaryLight"
            padding={false}
            rm={Decimal.ROUND_UP}
            // swap precision
            dp={feeDecimalsOffset(nativeToken?.precision)}
          >
            {info.dstGasFee!}
          </Text.numeral>
          <Text color="primaryLight">{nativeToken?.symbol}</Text>
          <Text className="oui-text-primary-light/60">
            ($
            <Text.numeral
              color="primaryLight"
              padding={false}
              rm={Decimal.ROUND_UP}
              dp={2}
            >
              {new Decimal(info.dstGasFee?.toString())
                .mul(nativePrice || 0)
                .toString()}
            </Text.numeral>
            )
          </Text>
        </Flex>
      ),
    },
    {
      label: t("transfer.swapDeposit.swapFee"),
      value: (
        <>
          <Text.numeral
            intensity={80}
            dp={feeDecimalsOffset(src?.displayDecimals)}
            rm={Decimal.ROUND_UP}
            padding={false}
          >
            {info.swapFee}
          </Text.numeral>
          {` ${src?.token} `}
          <Text className="oui-text-[#63666D]">
            ($
            <Text.numeral padding={false} rm={Decimal.ROUND_UP} dp={2}>
              {new Decimal(info.swapFee).mul(markPrice || 0).toString()}
            </Text.numeral>
            )
          </Text>
        </>
      ),
    },
    {
      label: t("transfer.swapDeposit.bridgeFee"),
      value: mode === SwapMode.Cross && (
        <>
          <Text.numeral
            intensity={80}
            dp={feeDecimalsOffset(src?.displayDecimals)}
            rm={Decimal.ROUND_UP}
            padding={false}
          >
            {info.bridgeFee}
          </Text.numeral>
          {` ${src?.token} `}
          <Text className="oui-text-[#63666D]">
            ($
            <Text.numeral padding={false} rm={Decimal.ROUND_UP} dp={2}>
              {new Decimal(info.bridgeFee).mul(markPrice || 0).toString()}
            </Text.numeral>
            )
          </Text>
        </>
      ),
    },
    {
      label: t("transfer.swapDeposit.minimumReceived"),
      value: (
        <>
          <Text.numeral
            dp={dst?.displayDecimals}
            rm={Decimal.ROUND_DOWN}
            padding={false}
          >
            {info.received}
          </Text.numeral>
          {` ${dst?.token}`}
        </>
      ),
    },
    {
      label: t("common.price"),
      value: (
        <>
          {`1 ${src?.token} = `}
          <Text.numeral dp={3} padding={false}>
            {info.price}
          </Text.numeral>
          {` ${dst?.token}`}
        </>
      ),
    },
    {
      label: t("transfer.slippage"),
      value: `${info.slippage}%`,
    },
  ];

  const listView = (
    <Flex direction="column" itemAlign="start" gapY={3}>
      {listData?.map((item) => {
        if (item.value) {
          return <ListItem {...item} key={item.label} />;
        }
      })}
    </Flex>
  );

  const swapButton = (
    <Flex justify="center" mt={8}>
      <Button className="oui-w-full lg:oui-w-[184px]" onClick={props.onConfirm}>
        {t("transfer.swapDeposit.confirmSwap")}
      </Button>
    </Flex>
  );

  return (
    <Box intensity={800}>
      {header}
      <SwapTime time={info.time ?? 0} />
      {divider}

      {viewMode === "details" && (
        <>
          {listView}
          {swapButton}
        </>
      )}
    </Box>
  );
};

type ListItemProps = {
  label?: string;
  value?: ReactNode;
};

const ListItem: FC<ListItemProps> = (props) => {
  return (
    <Flex gapX={2} justify="between" width="100%">
      <Text size="sm" intensity={54}>
        {props.label}
      </Text>
      <Text size="sm" intensity={98} as="div">
        {props.value}
      </Text>
    </Flex>
  );
};

type SwapSymbolProps = {
  primary: string;
  chainId?: number;
  component?: ReactNode;
  amount: string;
  dp?: number;
  symbol: string;
  reverse?: boolean;
};

const SwapSymbol: FC<SwapSymbolProps> = (props) => {
  return (
    <Flex gapX={2} direction={props.reverse ? "rowReverse" : "row"}>
      <Icon.combine
        secondary={{
          chainId: props.chainId,
          component: props.component,
          className: "oui-w-4 oui-h-4 oui-outline-base-8 oui-rounded-full",
        }}
      >
        <TokenIcon name={props.primary} size="md" />
      </Icon.combine>

      <Flex direction="column" itemAlign={props.reverse ? "end" : "start"}>
        <Text.numeral intensity={98} size="lg" dp={props.dp}>
          {props.amount}
        </Text.numeral>
        <Text intensity={54} size="xs">
          {props.symbol}
        </Text>
      </Flex>
    </Flex>
  );
};

const SwapTime: FC<{ time: number }> = (props) => {
  return (
    <Flex justify="center">
      <Flex
        intensity={600}
        r="full"
        gapX={2}
        justify="center"
        py={1}
        mt={2}
        width={210}
      >
        <Text size="sm" intensity={54}>
          {/* @ts-ignore */}
          <Trans
            i18nKey="transfer.swapDeposit.averageSwapTime"
            values={{ time: props.time }}
            components={[<Text color="primaryLight" key="0" />]}
          />
        </Text>
      </Flex>
    </Flex>
  );
};
