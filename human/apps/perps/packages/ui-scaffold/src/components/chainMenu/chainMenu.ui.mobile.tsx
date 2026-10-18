import { useTranslation } from "@paxeer-network/i18n";
import { Box, ChainIcon, modal, toast } from "@paxeer-network/ui";
import { ChainSelectorSheetId } from "@paxeer-network/ui-chain-selector";
import { UseChainMenuScriptReturn } from "./chainMenu.script";

export const ChainMenuUiMobile = (props: UseChainMenuScriptReturn) => {
  const { t } = useTranslation();

  return (
    <button
      onClick={(e) => {
        modal
          .show<{
            wrongNetwork: boolean;
          }>(ChainSelectorSheetId, {
            // networkId: props.networkId,
            bridgeLessOnly: false,
            isWrongNetwork: props.wrongNetwork,
            onChainChangeBefore: props.onChainChangeBefore,
            onChainChangeAfter: props.onChainChangeAfter,
          })
          .then(
            (r: any) => {
              console.log(r?.chainId, props);
              if (r?.chainId) {
                props.setCurrentChainId(r?.chainId);
              }
              toast.success(t("connector.networkSwitched"));
            },
            (error) => console.log("[switchChain error]", error),
          );
      }}
    >
      <Box className="oui-relative oui-rounded-t-[6px] oui-rounded-bl-[6px] oui-rounded-br-[3px] oui-bg-base-5 oui-px-2 oui-h-8 oui-w-8 oui-flex oui-items-center oui-justify-center">
        <ChainIcon
          chainId={props.currentChainId!}
          size="xs"
          className="oui-h-[18px] oui-w-[18px]"
        />
        <div className="oui-absolute oui-end-0 oui-bottom-0">
          <svg
            width="9"
            height="9"
            viewBox="0 0 9 9"
            fill="none"
            xmlns="http://www.w3.org/2000/svg"
          >
            <path d="M9 6V0L0 9h6a3 3 0 0 0 3-3" fill="url(#a)" />
            <defs>
              <linearGradient
                id="a"
                x1="9"
                y1="4.5"
                x2="0"
                y2="4.5"
                gradientUnits="userSpaceOnUse"
              >
                <stop stopColor="rgb(var(--oui-gradient-brand-end))" />
                <stop
                  offset="1"
                  stopColor="rgb(var(--oui-gradient-brand-start))"
                />
              </linearGradient>
            </defs>
          </svg>
        </div>
      </Box>
    </button>
  );
};
