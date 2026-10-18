import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Text, Button } from "@paxeer-network/ui";
import {
  BrokerWallet,
  QuantityInput,
  AvailableQuantity,
} from "@paxeer-network/ui-transfer";
import { LatestDepositWidget } from "../latest-deposit/latest-deposit.widget";
import { VaultDepositFormScript } from "./vault-deposit-form.script";

export const VaultDepositForm: FC<VaultDepositFormScript> = (props) => {
  const {
    quantity,
    onQuantityChange,
    sourceToken,
    maxQuantity,
    shares,
    handleDeposit,
    vaultId,
    disabledDeposit,
    disabledOperation,
    inputHint,
  } = props;

  const { t } = useTranslation();
  return (
    <div>
      <BrokerWallet />
      <Box mt={3} mb={1}>
        <QuantityInput
          value={quantity}
          onValueChange={onQuantityChange}
          token={{ ...sourceToken, precision: 6 } as any}
          testId="oui-testid-vault-deposit-dialog-quantity-input"
          hintMessage={inputHint.hintMessage}
          status={inputHint.status as any}
        />
      </Box>
      <AvailableQuantity
        maxQuantity={maxQuantity?.toString() || "0"}
        onClick={() => {
          onQuantityChange(maxQuantity?.toString() || "0");
        }}
      />
      <Box
        mt={5}
        mb={1}
        className="oui-flex oui-items-center oui-justify-between oui-text-sm oui-font-semibold oui-text-base-contrast-54"
      >
        <div>{t("vaults.deposit.estShares")}</div>
        <Text.numeral
          dp={6}
          padding={false}
          suffix={
            <span className="oui-ms-1 oui-text-base-contrast-54">
              {t("vaults.deposit.shares")}
            </span>
          }
        >
          {shares}
        </Text.numeral>
      </Box>
      <Box
        mb={5}
        className="oui-flex oui-items-center oui-justify-between oui-text-sm oui-font-semibold oui-text-base-contrast-54"
      >
        <div>{t("vaults.deposit.lockupDuration")}</div>
        <Text.numeral
          suffix={
            <span className="oui-ms-1 oui-text-base-contrast-54">hrs</span>
          }
        >
          48
        </Text.numeral>
      </Box>
      {disabledOperation && (
        <div className="oui-mt-3 oui-text-center">
          <Text color="warning" className="oui-text-sm oui-font-semibold">
            {t("vaults.operation.error.switchAccount")}
          </Text>
        </div>
      )}
      <Button
        fullWidth
        color="primary"
        disabled={disabledDeposit}
        className="oui-mt-3"
        onClick={handleDeposit}
      >
        {t("common.deposit")}
      </Button>
      <LatestDepositWidget vaultId={vaultId} />
    </div>
  );
};
