import { FC, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { VaultsHeaderScript } from "./vaults-header.script";

export const VaultsHeaderDesktop: FC<VaultsHeaderScript> = (props) => {
  const { supportVaults, headerImage, brokerName } = props;
  const { t } = useTranslation();

  const supportVaultsList = useMemo(() => {
    return (
      <div className="oui-flex oui-items-center">
        {supportVaults.map((chain, index) => (
          <img
            key={chain.chain_id}
            src={`/assets/static/network_logo/${chain.chain_id}.png`}
            alt={chain.chain_id}
            className="oui-relative oui-size-5"
            style={{
              marginLeft: index > 0 ? "-4px" : "0",
              zIndex: supportVaults.length - index,
            }}
          />
        ))}
      </div>
    );
  }, [supportVaults]);

  const headerImageElement = useMemo(() => {
    if (typeof headerImage === "string") {
      return (
        <img
          src={headerImage}
          alt="header"
          className="oui-bg-contain oui-bg-center oui-bg-no-repeat"
        />
      );
    }
    return headerImage;
  }, [headerImage]);

  return (
    <div className="oui-flex oui-items-center oui-justify-between">
      <div className="oui-flex oui-max-w-[726px] oui-flex-col">
        <div className="oui-flex oui-items-center oui-gap-1 oui-text-[18px] oui-font-normal oui-text-base-contrast-54">
          {t("vaults.availableOn")} {supportVaultsList}
        </div>
        <div className="oui-mb-6 oui-mt-8 oui-text-5xl oui-font-bold oui-leading-[44px] oui-text-base-contrast">
          {t("vaults.header.title")}
        </div>
        <div className="oui-text-xl oui-font-normal oui-text-base-contrast-54">
          {t("vaults.header.description", { brokerName })}
        </div>
      </div>
      <div className="oui-h-[238px] oui-w-[360px]">{headerImageElement}</div>
    </div>
  );
};
