import React from "react";
import { Trans, useTranslation } from "@paxeer-network/i18n";

const Link: React.FC<React.PropsWithChildren<{ url: string }>> = (props) => {
  const { url, children } = props;
  return (
    <span
      onClick={() => window.open(url)}
      className="oui-cursor-pointer oui-px-0.5 oui-text-primary-light oui-underline"
    >
      {children}
    </span>
  );
};

export const NoTradingview: React.FC = () => {
  const { t } = useTranslation();
  return (
    <div className="oui-absolute oui-inset-0 oui-z-0 oui-flex oui-flex-col oui-items-center oui-justify-start oui-p-2 oui-text-base-contrast-80 md:oui-justify-center md:oui-p-10">
      <div>
        <p className="oui-mb-6 oui-text-xs">{t("tradingView.noScriptSrc")}</p>

        <p className="oui-mb-3 oui-ps-0 oui-text-2xs oui-text-base-contrast-54 md:oui-ps-2 md:oui-text-base md:oui-text-base-contrast-80">
          <Trans
            i18nKey="tradingView.noScriptSrc.1"
            components={[
              <Link
                key={"tradingview-advanced-charts"}
                url="https://www.tradingview.com/advanced-charts"
              />,
            ]}
          />
        </p>
        <p className="oui-ps-0 oui-text-2xs oui-text-base-contrast-54 md:oui-ps-2 md:oui-text-base md:oui-text-base-contrast-80">
          <Trans
            i18nKey="tradingView.noScriptSrc.2"
            components={[
              <Link
                key={"tradingview-config"}
                url="https://paxeer-network.network/docs/sdks/react/components/trading#tradingviewconfig"
              />,
            ]}
          />
        </p>
      </div>
    </div>
  );
};
