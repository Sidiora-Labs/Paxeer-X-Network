import { FC, SVGProps, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import {
  Checkbox,
  Flex,
  PopoverContent,
  PopoverRoot,
  PopoverTrigger,
} from "@paxeer-network/ui";

export interface BuySellRatioSettingsProps {
  showBuySellRatio: boolean;
  setShowBuySellRatio?: (show: boolean) => void;
}

export const BuySellRatioSettings: FC<BuySellRatioSettingsProps> = ({
  showBuySellRatio,
  setShowBuySellRatio,
}) => {
  const { t } = useTranslation();
  const [settingsOpen, setSettingsOpen] = useState(false);

  return (
    <PopoverRoot open={settingsOpen} onOpenChange={setSettingsOpen}>
      <PopoverTrigger asChild>
        <button
          className="oui-cursor-pointer oui-text-base-contrast-54 hover:oui-text-base-contrast oui-transition-colors"
          aria-label={t("trading.orderBook.settings")}
        >
          <MoreIcon />
        </button>
      </PopoverTrigger>
      <PopoverContent align="end" sideOffset={8} className="oui-w-[200px] ">
        <Flex itemAlign="center" gap={2}>
          <Checkbox
            id="show-buy-sell-ratio"
            checked={showBuySellRatio}
            onCheckedChange={(checked) => {
              setShowBuySellRatio?.(checked === true);
              setSettingsOpen(false);
            }}
          />
          <label
            htmlFor="show-buy-sell-ratio"
            className="oui-cursor-pointer oui-text-xs oui-text-base-contrast-54"
          >
            {t("trading.orderBook.showBuySellRatio")}
          </label>
        </Flex>
      </PopoverContent>
    </PopoverRoot>
  );
};

const MoreIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    xmlns="http://www.w3.org/2000/svg"
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    {...props}
  >
    <path d="M8.00521 6.66797C8.74161 6.66797 9.33854 7.26464 9.33854 8.0013C9.33854 8.73797 8.74161 9.33464 8.00521 9.33464C7.26881 9.33464 6.67188 8.73797 6.67188 8.0013C6.67188 7.26464 7.26881 6.66797 8.00521 6.66797Z" />
    <path d="M3.33333 6.66797C4.06973 6.66797 4.66667 7.26464 4.66667 8.0013C4.66667 8.73797 4.06973 9.33464 3.33333 9.33464C2.59693 9.33464 2 8.73797 2 8.0013C2 7.26464 2.59693 6.66797 3.33333 6.66797Z" />
    <path d="M12.6666 6.66797C13.403 6.66797 14 7.26464 14 8.0013C14 8.73797 13.403 9.33464 12.6666 9.33464C11.9302 9.33464 11.3333 8.73797 11.3333 8.0013C11.3333 7.26464 11.9302 6.66797 12.6666 6.66797Z" />
  </svg>
);
