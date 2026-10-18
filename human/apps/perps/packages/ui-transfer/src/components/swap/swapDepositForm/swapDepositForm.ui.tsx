import { FC } from "react";
import { Box, Flex, textVariants } from "@paxeer-network/ui";
import { ActionButton } from "../../actionButton";
import { AvailableQuantity } from "../../availableQuantity";
import { BrokerWallet } from "../../brokerWallet";
import { ChainSelect } from "../../chainSelect";
import { ExchangeDivider } from "../../exchangeDivider";
import { QuantityInput } from "../../quantityInput";
import { Slippage } from "../../slippage";
import { SwapCoin } from "../../swapCoin";
import { Web3Wallet } from "../../web3Wallet";
import { Notice } from "../components/notice";
import { SwapFee } from "../components/swapFee";
import { UseSwapDepositFormScriptReturn } from "./swapDepositForm.script";

export const SwapDepositForm: FC<UseSwapDepositFormScriptReturn> = (props) => {
  const {
    token,
    tokens,
    onTokenChange,
    amount,
    quantity,
    maxQuantity,
    swapQuantity,
    onQuantityChange,
    hintMessage,
    inputStatus,
    chains,
    currentChain,
    settingChain,
    onChainChange,
    actionType,
    onDeposit,
    onApprove,
    dst,
    wrongNetwork,
    balanceRevalidating,
    loading,
    disabled,
    networkId,
    slippage,
    onSlippageChange,
    needSwap,
    needCrossSwap,
    swapPrice,
    swapRevalidating,
    warningMessage,
    fee,
  } = props;

  return (
    <Box
      id="oui-swap-deposit-form"
      className={textVariants({ weight: "semibold" })}
    >
      <Box className="oui-mb-6 lg:oui-mb-8">
        <Web3Wallet />

        <Box mt={3} mb={1}>
          <ChainSelect
            chains={chains}
            value={currentChain!}
            onValueChange={onChainChange}
            wrongNetwork={wrongNetwork}
            loading={settingChain}
          />
          <QuantityInput
            classNames={{
              root: "oui-mt-[2px] oui-rounded-t-sm oui-rounded-b-xl",
            }}
            value={quantity}
            onValueChange={onQuantityChange}
            tokens={tokens}
            token={token}
            onTokenChange={onTokenChange}
            status={inputStatus}
            hintMessage={hintMessage}
            data-testid="oui-testid-swap-deposit-dialog-quantity-input"
            showDisplayName
          />
        </Box>

        <AvailableQuantity
          token={token}
          quantity={amount}
          maxQuantity={maxQuantity}
          loading={balanceRevalidating}
          onClick={() => {
            onQuantityChange(maxQuantity);
          }}
          showDisplayName
        />
        <ExchangeDivider />

        <BrokerWallet />

        <QuantityInput
          readOnly
          token={dst as any}
          value={swapQuantity}
          loading={swapRevalidating}
          classNames={{
            root: "oui-mt-3 oui-border-transparent focus-within:oui-outline-transparent",
          }}
        />

        <Flex direction="column" itemAlign="start" mt={1} gapY={1}>
          <Flex justify="between" width="100%">
            {/* <SwapCoin token={token as any} dst={dst} price={swapPrice} /> */}
            {/* <SwapCoin
              sourceSymbol={token as any}
              targetSymbol={dst.symbol}
              indexPrice={swapPrice}
            /> */}
            {(needSwap || needCrossSwap) && (
              <Slippage value={slippage} onValueChange={onSlippageChange} />
            )}
          </Flex>

          <SwapFee {...fee} />
        </Flex>
      </Box>

      <Notice
        message={warningMessage}
        needSwap={needSwap}
        needCrossSwap={needCrossSwap}
        wrongNetwork={wrongNetwork}
        networkId={networkId}
      />

      <Flex justify="center">
        <ActionButton
          actionType={actionType}
          symbol={token?.symbol}
          disabled={disabled}
          loading={loading}
          onDeposit={onDeposit}
          onApprove={onApprove}
          networkId={networkId}
        />
      </Flex>
    </Box>
  );
};
