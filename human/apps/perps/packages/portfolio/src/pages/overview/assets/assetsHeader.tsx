import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  ArrowDownSquareFillIcon,
  ArrowLeftRightIcon,
  ArrowUpSquareFillIcon,
  Button,
  CardTitle,
  Flex,
} from "@paxeer-network/ui";

type Props = {
  disabled: boolean;
  onWithdraw?: () => void;
  onDeposit?: () => void;
  onTransfer?: () => void;
  isMainAccount?: boolean;
  hasSubAccount?: boolean;
};

export const AssetsHeader: FC<Props> = (props) => {
  const { t } = useTranslation();

  return (
    <Flex justify={"between"}>
      <CardTitle>{t("common.overview")}</CardTitle>
      <Flex gap={3}>
        {props.isMainAccount && (
          <Button
            disabled={props.disabled}
            size="md"
            onClick={() => props.onDeposit?.()}
            icon={
              <ArrowDownSquareFillIcon className="oui-text-primary-contrast" />
            }
            data-testid="oui-testid-portfolio-assets-deposit-btn"
          >
            {t("common.deposit")}
          </Button>
        )}
        {props.hasSubAccount && (
          <Button
            disabled={props.disabled}
            size="md"
            color="secondary"
            onClick={() => props.onTransfer?.()}
            icon={<ArrowLeftRightIcon className="oui-text-base-contrast" />}
          >
            {t("common.transfer")}
          </Button>
        )}
        {props.isMainAccount && (
          <Button
            disabled={props.disabled}
            size="md"
            color="secondary"
            onClick={() => props.onWithdraw?.()}
            icon={<ArrowUpSquareFillIcon className="oui-text-base-contrast" />}
            data-testid="oui-testid-portfolio-assets-withdraw-btn"
          >
            {t("common.withdraw")}
          </Button>
        )}
      </Flex>
    </Flex>
  );
};
