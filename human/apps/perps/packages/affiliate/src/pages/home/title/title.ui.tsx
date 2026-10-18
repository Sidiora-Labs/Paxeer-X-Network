import React from "react";
import { useTranslation } from "@paxeer-network/i18n";

export const Title: React.FC = () => {
  const { t } = useTranslation();
  return (
    <div
      id="oui-affiliate-home-title"
      className="oui-text-center oui-text-3xl oui-font-bold md:oui-text-3xl lg:oui-text-4xl xl:oui-text-5xl"
    >
      {t("affiliate.page.title")}
    </div>
  );
};
