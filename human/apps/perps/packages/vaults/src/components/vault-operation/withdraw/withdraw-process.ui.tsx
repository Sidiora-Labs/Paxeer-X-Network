import { FC } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Text, cn } from "@paxeer-network/ui";

const ProcessDot = () => {
  return (
    <div className="oui-size-2 oui-rounded-full oui-bg-base-contrast-54" />
  );
};

const ProcessLine = () => {
  return <div className="oui-h-px oui-flex-1 oui-bg-base-contrast-20" />;
};

const ProcessText = ({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) => {
  return (
    <div
      className={cn(
        "oui-flex-1 oui-text-2xs oui-font-normal oui-text-base-contrast-80",
        className,
      )}
    >
      {children}
    </div>
  );
};

const WithdrawProcess: FC = () => {
  const { t } = useTranslation();
  return (
    <>
      <div className="oui-flex oui-items-center">
        <ProcessDot />
        <div className="oui-flex-1">
          <ProcessLine />
        </div>
        <ProcessDot />
        <div className="oui-flex-1">
          <ProcessLine />
        </div>
        <ProcessDot />
      </div>
      <div className="oui-mt-1 oui-flex oui-justify-between">
        <ProcessText>{t("vaults.withdraw.process.initiate")}</ProcessText>
        <ProcessText className="oui-text-center">
          {t("vaults.withdraw.process.vaultProcess")}
        </ProcessText>
        <ProcessText className="oui-text-end">
          {t("vaults.withdraw.process.transferred")}
        </ProcessText>
      </div>
    </>
  );
};

export const WithdrawProcessWidget: FC = () => {
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-flex-col oui-gap-2 oui-rounded-xl oui-border oui-border-line-12 oui-p-3">
      <div className="oui-flex oui-items-center oui-justify-between oui-text-sm oui-font-semibold oui-text-base-contrast-54">
        <div>{t("vaults.withdraw.process.withdrawProcess")}</div>
        <Text color="primary">{t("vaults.withdraw.process.upTo")}</Text>
      </div>
      <WithdrawProcess />
    </div>
  );
};
