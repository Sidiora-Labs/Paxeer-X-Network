import { FC, useMemo } from "react";
import { cn, useScreen } from "@paxeer-network/ui";
import { useCanTrade } from "../../../hooks/useCanTrade";
import { CampaignsContentDesktopUI } from "./campaigns.content.desktop.ui";
import { useCampaignsScript } from "./campaigns.script";
import { CampaignsTimeDesktopUI } from "./components/time.desktop.ui";
import { CampaignsHeaderWidget } from "./header";

export type CampaignsWidgetProps = {
  className?: string;
  style?: React.CSSProperties;
  hideCampaignsBanner?: boolean;
};

export const CampaignsWidget: FC<CampaignsWidgetProps> = (props) => {
  const state = useCampaignsScript();
  const { isMobile } = useScreen();

  const canTrade = useCanTrade();

  const emphasisConfig = state.currentCampaign?.emphasisConfig;

  const contentClassNames = useMemo(() => {
    if (emphasisConfig?.subtitle) {
      return {
        container: !canTrade
          ? "!oui-h-[680px] lg:!oui-h-[700px]"
          : "!oui-h-[600px] lg:!oui-h-[700px]",
        topContainer: "!oui-w-[90%]",
      };
    }

    if (!isMobile) return undefined;
    return {
      container: cn("oui-h-[400px] oui-gap-5"),
      time: "oui-text-sm oui-h-5",
      title: "oui-text-[24px] oui-leading-[32px]",
      description: "oui-text-2xs oui-leading-[15px]",
      topContainer: cn("oui-w-[284px] oui-gap-1"),
    };
  }, [isMobile, emphasisConfig, canTrade]);

  return (
    <div
      className={cn(["oui-relative oui-z-[1] oui-overflow-hidden"])}
      style={props.style}
    >
      {!props.hideCampaignsBanner && (
        <CampaignsHeaderWidget
          backgroundSrc={state.backgroundSrc}
          campaigns={state.campaigns}
          currentCampaignId={state.currentCampaignId.toString()}
          onCampaignChange={state.onCampaignChange}
        />
      )}
      {state.currentCampaign && (
        <CampaignsContentDesktopUI
          {...state}
          classNames={contentClassNames}
          isMobile={isMobile}
          campaign={state.currentCampaign}
        />
      )}
      {state.currentCampaign && (
        <CampaignsTimeDesktopUI
          campaign={state.currentCampaign}
          isMobile={isMobile}
        />
      )}
    </div>
  );
};
