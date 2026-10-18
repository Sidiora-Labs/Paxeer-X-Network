import { useId } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Checkbox, cn, InfoCircleIcon, Tips } from "@paxeer-network/ui";
import { useLowVolumeMarketFilter } from "./lowVolumeMarketFilter";

export const ShowLowVolumeFilter = ({ className }: { className?: string }) => {
  const id = useId();
  const { t } = useTranslation();
  const { showLowVolume, setShowLowVolume } = useLowVolumeMarketFilter();

  const tooltipContent = t("markets.showLowVolume.tooltip");
  return (
    <div
      className={cn(
        "oui-markets-lowVolumeFilter oui-flex oui-shrink-0 oui-items-center oui-gap-x-1",
        className,
      )}
    >
      <label
        htmlFor={id}
        className="oui-inline-flex oui-size-4 oui-shrink-0 oui-cursor-pointer oui-items-center oui-justify-center"
      >
        <Checkbox
          id={id}
          checked={showLowVolume}
          onCheckedChange={(checked) => setShowLowVolume(checked === true)}
        />
      </label>
      <label
        htmlFor={id}
        className="oui-cursor-pointer oui-whitespace-nowrap oui-text-xs oui-text-base-contrast-54"
      >
        {t("markets.showLowVolume")}
      </label>
      <Tips
        title={t("common.tips")}
        content={tooltipContent}
        className="oui-inline-flex"
        trigger={
          <InfoCircleIcon
            opacity={1}
            aria-label={tooltipContent}
            className="oui-size-4 oui-cursor-pointer oui-text-base-contrast-54"
          />
        }
      />
    </div>
  );
};
