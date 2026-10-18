import { FC, useMemo, useState } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { API } from "@paxeer-network/types";
import {
  BellIcon,
  DialogHeader,
  DialogBody,
  DialogContent,
  Dialog,
  Popover,
  Divider,
  DialogTitle,
  TooltipProvider,
  TooltipRoot,
  TooltipTrigger,
  TooltipContent,
} from "@paxeer-network/ui";
import { AnnouncementCenterUI } from "@paxeer-network/ui-notification";
import { useScaffoldContext } from "../scaffold";

/**
 * Notify bell with Popover (click) + Tooltip (hover) coexisting.
 * - We control Popover open state; when Popover is open, we disable Tooltip to prevent hover/click conflicts.
 * - Both triggers use `asChild` and stack on the same real DOM element to avoid extra wrappers.
 */
export const MessageCenter: FC<{
  maintenanceDialogInfo?: string;
  messages: API.AnnouncementRow[];
  showAnnouncement: boolean;
  closeTips: () => void;
}> = (props) => {
  const { maintenanceDialogInfo, messages, showAnnouncement, closeTips } =
    props;
  const { routerAdapter } = useScaffoldContext();
  const { t } = useTranslation();
  const messageSize = useMemo(() => messages.length, [messages]);
  // Track Popover open to suppress Tooltip while the popover is visible
  const [popoverOpen, setPopoverOpen] = useState(false);

  const onItemClick = (url: string) => {
    if (!url) return;
    routerAdapter?.onRouteChange({
      href: url,
      name: url,
      target: "_blank",
    });
  };

  return (
    <>
      {!!maintenanceDialogInfo && (
        <Dialog open>
          <DialogContent
            closable={false}
            onOpenAutoFocus={(event) => event.preventDefault()}
            className="oui-w-[320px] lg:oui-w-auto"
          >
            <DialogHeader>
              <DialogTitle>{t("maintenance.dialog.title")}</DialogTitle>
            </DialogHeader>
            <Divider />
            <DialogBody className="oui-text-2xs lg:oui-text-xs">
              {maintenanceDialogInfo}
            </DialogBody>
          </DialogContent>
        </Dialog>
      )}
      <TooltipProvider delayDuration={250}>
        {/* When the Popover is open, force Tooltip closed to avoid dual overlays */}
        <TooltipRoot open={popoverOpen ? false : undefined}>
          <Popover
            open={popoverOpen}
            onOpenChange={(open) => {
              setPopoverOpen(open);
              if (open) {
                closeTips();
              }
            }}
            arrow
            content={
              <AnnouncementCenterUI
                dataSource={messages}
                onItemClick={onItemClick}
              />
            }
            contentProps={{
              align: "end",
              className: "oui-w-[360px] oui-shadow-xl oui-p-0",
            }}
          >
            {/* Stack TooltipTrigger and PopoverTrigger (via Popover's asChild) on the same element */}
            <TooltipTrigger asChild>
              <div className="oui-relative oui-flex oui-cursor-pointer oui-items-center oui-justify-center">
                <BellIcon
                  color="white"
                  opacity={0.8}
                  size={20}
                  viewBox="0 0 20 20"
                  className="hover:oui-opacity-100"
                />
                {messageSize > 0 && showAnnouncement && (
                  <div className="oui-absolute -oui-end-[6px] -oui-top-[6px] oui-flex oui-size-4 oui-items-center oui-justify-center oui-rounded-full oui-bg-primary-darken oui-text-[10px] oui-leading-none oui-text-primary-contrast">
                    {messageSize}
                  </div>
                )}
              </div>
            </TooltipTrigger>
          </Popover>
          <TooltipContent side="bottom">
            {t("ui.messageCenter.tooltip")}
          </TooltipContent>
        </TooltipRoot>
      </TooltipProvider>
    </>
  );
};
