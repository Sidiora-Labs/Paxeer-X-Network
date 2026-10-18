import React from "react";
import { cn } from "@paxeer-network/ui";
import { TradingviewWidget } from "@paxeer-network/ui-tradingview";
import { KlineDragIcon } from "../../base/icons";
import type { TradingviewState } from "./tradingview.script";

export const TradingviewUI: React.FC<TradingviewState> = (props) => {
  return (
    <div
      className="oui-trading-tradingview oui-pb-1 oui-flex oui-flex-col oui-gap-1"
      style={{ height: props.height }}
      ref={props.boxRef}
    >
      <div className="oui-w-full oui-h-full">
        <TradingviewWidget
          symbol={props.symbol}
          libraryPath={props.tradingViewConfig?.library_path}
          mode={3}
          scriptSRC={props.tradingViewConfig?.scriptSRC}
          customCssUrl={props.tradingViewConfig?.customCssUrl}
          {...props.tradingViewConfig}
        />
      </div>
      <div className="oui-relative oui-w-full">
        <div
          ref={props.dragRef}
          className={cn(
            "oui-h-[1px] oui-absolute oui-start-0 oui-end-0 oui-bottom-0 oui-top-0 oui-z-10 oui-mt-[7px] oui-bg-base-contrast-12",
            props.dragging && "oui-bg-primary ",
          )}
        >
          <KlineDragIcon
            className={cn(
              "oui-w-3 oui-h-3 oui-absolute oui-start-1/2 -oui-top-[5px] -oui-translate-y-[0.5px] oui-text-base-contrast-12",
              props.dragging && " oui-text-primary",
            )}
          />
        </div>
      </div>
    </div>
  );
};
