import { FC, ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  Input,
  Select,
  InputProps,
  cn,
  Box,
  Text,
  Flex,
  inputFormatter,
  Spinner,
  InputFormatter,
  useDocumentDirection,
} from "@paxeer-network/ui";
import { Decimal } from "@paxeer-network/utils";
import { InputStatus } from "../../types";
import { TokenOption } from "./tokenOption";

export type QuantityInputProps = {
  token?: API.TokenInfo;
  tokens?: API.TokenInfo[];
  label?: string;
  status?: InputStatus;
  hintMessage?: string;
  hintSuffix?: ReactNode;
  onValueChange?: (value: string) => void;
  onTokenChange?: (token: any) => void;
  loading?: boolean;
  testId?: string;
  formatters?: InputFormatter[];
  // TOOD: remove this prop
  vaultBalanceList?: API.VaultBalance[];
  displayType?: "balance" | "vaultBalance";
  tokenValueFormatter?: (value: string) => ReactNode;
  tokenShowCaret?: boolean;
  balancesRevalidating?: boolean;
  showBalance?: boolean;
  /**
   * Token label display mode.
   * - `false` (default): platform-side paxeer-network account assets — symbol only
   * - `true`: wallet-side chain assets — `display_name || symbol`
   */
  showDisplayName?: boolean;
} & Omit<InputProps, "onClear" | "suffix" | "onValueChange">;

export const QuantityInput: FC<QuantityInputProps> = (props) => {
  const {
    token,
    tokens = [],
    classNames,
    label,
    status,
    hintMessage,
    hintSuffix,
    value,
    onValueChange,
    onTokenChange,
    loading,
    placeholder,
    formatters,
    vaultBalanceList,
    displayType,
    tokenValueFormatter,
    tokenShowCaret,
    balancesRevalidating,
    showBalance,
    showDisplayName = false,
    ...rest
  } = props;

  const { t } = useTranslation();
  const dir = useDocumentDirection();

  const inputRef = useRef<HTMLInputElement>(null);
  const [open, setOpen] = useState(false);
  const [width, setWidth] = useState(0);

  const getTokenLabel = (tokenInfo: API.TokenInfo) =>
    showDisplayName
      ? tokenInfo.display_name || tokenInfo.symbol!
      : tokenInfo.symbol!;

  const tokenOptions = useMemo(() => {
    return tokens.map((token) => {
      const currentToken = vaultBalanceList?.find(
        (item) => item.token === token.symbol,
      );
      const insufficientBalance = currentToken
        ? new Decimal(currentToken.balance ?? 0).lt(value ? Number(value) : 0)
        : false;
      return {
        ...token,
        name: getTokenLabel(token),
        insufficientBalance,
      };
    });
  }, [tokens, value, vaultBalanceList, showDisplayName]);

  useEffect(() => {
    const rect = inputRef?.current?.getBoundingClientRect();
    setWidth(rect?.width || 0);
  }, [inputRef]);

  const _onTokenChange = (value: string) => {
    const find =
      tokens.find((item) => getTokenLabel(item) === value) ??
      tokens.find((item) => item.symbol === value);
    if (find) {
      onTokenChange?.(find);
    }
  };

  const selectable = tokens.length > 1;
  const selectOpen = selectable ? open : false;

  const optionRenderer = (item: any) => {
    const isActive = item.symbol === token?.symbol;
    return (
      <TokenOption
        token={item}
        isActive={isActive}
        displayType={displayType}
        onTokenChange={(item) => {
          onTokenChange?.(item);
          setOpen(false);
        }}
        open={selectOpen}
        isLoading={balancesRevalidating}
        showBalance={showBalance}
      />
    );
  };

  const prefix = (
    <Box>
      <Box className="oui-absolute oui-top-0">
        <Text size="2xs" intensity={36}>
          {label || t("common.quantity")}
        </Text>
      </Box>
      {loading && (
        <Box className="oui-absolute oui-bottom-1">
          <Spinner size="sm" />
        </Box>
      )}
    </Box>
  );

  const tokenLabel = token ? getTokenLabel(token) : undefined;

  const suffix = (
    <div className="oui-absolute oui-end-0">
      <Select.tokens
        open={selectOpen}
        onOpenChange={setOpen}
        disabled={rest.disabled}
        variant="text"
        tokens={tokenOptions}
        value={tokenLabel}
        size={rest.size}
        onValueChange={_onTokenChange}
        showIcon
        valueFormatter={tokenValueFormatter}
        showCaret={tokenShowCaret}
        optionRenderer={optionRenderer}
        contentProps={{
          onCloseAutoFocus: (event) => {
            event.preventDefault();
            inputRef.current?.focus();
          },
          onClick: (event) => {
            event.preventDefault();
            inputRef.current?.focus();
          },
          style: { width },
          align: dir === "rtl" ? "start" : "end",
          sideOffset: 5,
          className: "oui-border oui-border-line-6",
        }}
      />
    </div>
  );

  const message = (
    <Flex mt={1} gapX={1} px={1} justify="between" itemAlign="center">
      <Flex gapX={1} itemAlign="center">
        <Box
          width={4}
          height={4}
          r="full"
          className={cn(
            status === "error" && "oui-bg-danger-light",
            status === "warning" && "oui-bg-warning-light",
          )}
        ></Box>
        <Text
          size="2xs"
          className={cn(
            status === "error" && "oui-text-danger-light",
            status === "warning" && "oui-text-warning-light",
          )}
        >
          {hintMessage}
        </Text>
      </Flex>
      {hintSuffix && (
        <Box className="oui-flex oui-items-center oui-gap-1">{hintSuffix}</Box>
      )}
    </Flex>
  );

  const _placeholder = placeholder ?? (loading ? "" : "0");

  return (
    <>
      <Input
        ref={inputRef}
        autoComplete="off"
        placeholder={_placeholder}
        prefix={prefix}
        suffix={suffix}
        value={value}
        onValueChange={(value) => {
          onValueChange?.(value);
        }}
        formatters={
          formatters || [
            inputFormatter.numberFormatter,
            inputFormatter.dpFormatter(token?.precision ?? 2),
            inputFormatter.currencyFormatter,
          ]
        }
        {...rest}
        classNames={{
          ...classNames,
          root: cn(
            "oui-relative oui-h-[54px] oui-px-3",
            "oui-rounded-lg",
            status === "error" &&
              "oui-outline-danger-light focus-within:oui-outline-danger-light",
            status === "warning" &&
              "oui-outline-warning-light focus-within:oui-outline-warning-light",
            props.readOnly && "focus-within:oui-outline-0",
            classNames?.root,
          ),
          input: cn("oui-absolute oui-bottom-0", classNames?.input),
        }}
      />
      {hintMessage && message}
    </>
  );
};
