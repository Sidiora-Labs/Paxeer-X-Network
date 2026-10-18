import { FC, useMemo } from "react";
import { useConfig } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { VaultsHeaderScript } from "./vaults-header.script";

export const VaultsHeaderMobile: FC<VaultsHeaderScript> = (props) => {
  const { supportVaults, headerImage } = props;
  const { t } = useTranslation();
  const brokerName = useConfig("brokerName");

  const supportVaultsList = useMemo(() => {
    return (
      <div className="oui-flex oui-items-center">
        {supportVaults.map((chain, index) => (
          <img
            key={chain.chain_id}
            src={`/assets/static/network_logo/${chain.chain_id}.png`}
            alt={chain.chain_id}
            className="oui-relative oui-size-[18px]"
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
    <div className="oui-relative">
      <div className="oui-flex oui-flex-col">
        <div className="oui-flex oui-items-center oui-gap-1 oui-text-2xs oui-font-semibold oui-text-base-contrast-54">
          {t("vaults.availableOn")} {supportVaultsList}
        </div>
        <div className="oui-mb-4 oui-mt-6 oui-min-h-[56px] oui-max-w-[calc(100%-187px)] oui-text-xl oui-font-bold oui-text-base-contrast">
          {t("vaults.header.title")}
        </div>
        <div className="oui-max-w-[450px] oui-text-2xs oui-font-normal oui-text-base-contrast-54">
          {t("vaults.header.description", { brokerName })}
        </div>
      </div>
      <div className="oui-absolute -oui-end-3 -oui-top-4 oui-h-[124px] oui-w-[187px]">
        {headerImageElement}
      </div>
    </div>
  );
};
