import React, { FC, PropsWithChildren, SVGProps, useState } from "react";
import { Box, cn, Flex, Tooltip } from "@paxeer-network/ui";

export type RemovablePanelProps = {
  className?: string;
  index: number;
  onLayout: (currentIdx: number, targetIdx: number) => void;
  showIndicator?: boolean;
};

/** @deprecated unused */
export const RemovablePanel: React.FC<
  PropsWithChildren<RemovablePanelProps>
> = (props) => {
  const { showIndicator = true } = props;
  const [open, setOpen] = useState(false);
  return (
    <Box
      intensity={900}
      r="2xl"
      p={3}
      width="100%"
      className={cn("oui-relative", props.className)}
    >
      {props.children}
      <Tooltip
        open={open}
        onOpenChange={(next) => {
          // controlled by click
          if (!next) setOpen(false);
        }}
        side="left"
        align="start"
        sideOffset={-4}
        alignOffset={-4}
        content={
          <Flex direction="column" gapY={2}>
            {[TopIcon, MiddleIcon, BottomIcon].map((Icon, idx) => (
              <Icon
                key={idx}
                className={cn(
                  "oui-rounded oui-cursor-pointer hover:oui-bg-base-5",
                  props.index === idx && "oui-bg-base-5",
                )}
                onClick={() => {
                  props.onLayout?.(props.index, idx);
                  setOpen(false);
                }}
              />
            ))}
          </Flex>
        }
        delayDuration={0}
        className={cn(
          "oui-bg-base-9 oui-rounded",
          "oui-border oui-border-line-12",
          "oui-p-1",
        )}
        arrow={{ className: "oui-fill-transparent" }}
      >
        {showIndicator && (
          <div
            className="oui-absolute oui-end-[1px] oui-top-[18px]"
            onClick={() => setOpen((v) => !v)}
          >
            <IndicatorIcon
              className={cn(
                "oui-text-base-contrast-20 hover:oui-text-base-contrast-80",
                "oui-cursor-pointer",
              )}
            />
          </div>
        )}
      </Tooltip>
    </Box>
  );
};

export const IndicatorIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="10"
    height="16"
    viewBox="0 0 10 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect x="2" y="2" width="6" height="2" rx="1" />
    <rect x="2" y="7" width="6" height="2" rx="1" />
    <rect x="2" y="12" width="6" height="2" rx="1" />
  </svg>
);

export const TopIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect
      x="1.333"
      y="2.667"
      width="13.333"
      height="4"
      rx="2"
      fill="rgb(var(--oui-color-primary))"
    />
    <rect
      x="2.667"
      y="12"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
    <rect
      x="2.667"
      y="8.667"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
  </svg>
);

export const MiddleIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect
      x="1.333"
      y="6"
      width="13.333"
      height="4"
      rx="2"
      fill="rgb(var(--oui-color-primary))"
    />
    <rect
      x="2.667"
      y="12"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
    <rect
      x="2.667"
      y="2.667"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
  </svg>
);

export const BottomIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="none"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <rect
      x="1.333"
      y="9.333"
      width="13.333"
      height="4"
      rx="2"
      fill="rgb(var(--oui-color-primary))"
    />
    <rect
      x="2.667"
      y="6"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
    <rect
      x="2.667"
      y="2.667"
      width="10.667"
      height="1.333"
      rx=".667"
      fill="rgb(var(--oui-color-base-foreground))"
      fillOpacity=".2"
    />
  </svg>
);
