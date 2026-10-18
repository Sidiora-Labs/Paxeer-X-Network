import { FC } from "react";
import { cn } from "@paxeer-network/ui";

interface RiskIndicatorProps {
  size?: number;
  className?: string;
}

export const RiskIndicator: FC<RiskIndicatorProps> = (props) => {
  const { size = 20, className } = props;

  return (
    <div
      className="oui-flex oui-justify-center oui-items-end"
      style={{
        background: "url(/images/riskLevelBg.png)",
        backgroundSize: "contain",
        backgroundRepeat: "no-repeat",
        backgroundPosition: "center bottom",
        width: `${size}px`,
        height: `14px`,
      }}
    >
      <div
        className={cn(
          "oui-relative oui-w-[2px] oui-h-[2px] oui-rounded-full oui-bg-base-contrast after:oui-block after:oui-content-[''] after:oui-absolute after:oui-end-0 after:oui-w-[10px] after:oui-h-[2px] after:oui-bg-base-contrast-50",
          className,
          // {
          //   "oui-rotate-0": value > 1,
          //   "oui-rotate-90": value >= 0.05 && value <= 1,
          //   "oui-rotate-180": value < 0.05,
          // }
        )}
      ></div>
    </div>
  );
};
