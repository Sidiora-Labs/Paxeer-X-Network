import { useState } from "react";
import {
  PopoverContent,
  PopoverRoot,
  PopoverTrigger,
} from "@paxeer-network/ui";
import { AdditionalInfo, AdditionalInfoProps } from "./additionalInfo";

export function AdditionalConfigButton(props: AdditionalInfoProps) {
  const [open, setOpen] = useState(false);

  return (
    <PopoverRoot open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <button
          data-testid="oui-testid-orderEntry-additional-button"
          className="oui-orderEntry-additionalConfig-btn"
          onClick={() => {
            setOpen(true);
          }}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 16 16"
            fill="currentColor"
            xmlns="http://www.w3.org/2000/svg"
            className="oui-fill-base-contrast-36 hover:oui-fill-base-contrast-80"
          >
            <path
              d="M3.332 2.665a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V3.332a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V3.332a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V3.332a.667.667 0 0 0-.667-.667zm-8 4a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V7.332a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V7.332a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667V7.332a.667.667 0 0 0-.667-.667zm-8 4a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667v-1.333a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667v-1.333a.667.667 0 0 0-.667-.667zm4 0a.667.667 0 0 0-.667.667v1.333c0 .368.299.667.667.667h1.333a.667.667 0 0 0 .667-.667v-1.333a.667.667 0 0 0-.667-.667z"
              // fill="#fff"
              // fillOpacity={open ? 0.8 : 0.36}
            />
          </svg>
        </button>
      </PopoverTrigger>
      <PopoverContent
        side={"top"}
        align={"end"}
        className={
          "oui-orderEntry-additionalInfo-popover-content oui-w-[230px]"
        }
      >
        <AdditionalInfo {...props} />
      </PopoverContent>
    </PopoverRoot>
  );
}
