import React from "react";
import { cn, useScreen } from "@paxeer-network/ui";
import { PrivyConnectorImagePath } from "../../util";

export function AbstractConnectArea({ connect }: { connect: () => void }) {
  const { isMobile } = useScreen();
  return (
    <div>
      <div className="oui-mb-2 oui-text-sm oui-font-semibold oui-text-base-contrast-80">
        Abstract
      </div>
      <div className="oui-grid oui-grid-cols-2 oui-gap-2">
        <div
          className={cn(
            " oui-flex oui-flex-1 oui-cursor-pointer oui-items-center oui-justify-start oui-gap-1 oui-rounded-[6px]  oui-px-2 oui-py-[11px]",
            isMobile ? "oui-bg-base-5" : "oui-bg-base-10",
          )}
          onClick={() => connect()}
        >
          <div className="oui-flex oui-size-[18px] oui-items-center oui-justify-center">
            <img
              className={cn("oui-size-[12px]")}
              src={`${PrivyConnectorImagePath}/abstract.png`}
              alt="abstract wallet"
            />
          </div>
          <div className="oui-text-2xs oui-text-base-contrast">Abstract</div>
        </div>
      </div>
    </div>
  );
}
