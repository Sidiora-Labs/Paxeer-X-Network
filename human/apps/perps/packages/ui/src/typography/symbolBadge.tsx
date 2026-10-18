import React, { useState } from "react";
import { Badge } from "../badge/badge";
import { useScreen } from "../hooks";
import { Popover } from "../popover";
import { Tooltip } from "../tooltip/tooltip";
import { FormattedText, FormattedTextProps } from "./formatted";
import { TextElement } from "./text";

export type SymbolBadgeTextProps = FormattedTextProps & {
  /**
   * Optional badge text shown after the main content. When set, renders as Badge (warning, xs).
   */
  badge?: string;
  /** Full broker name; when set, badge shows it on hover via Tooltip. */
  fullName?: string;
  formatString?: string;
};

export const SymbolBadgeText = React.forwardRef<
  TextElement,
  SymbolBadgeTextProps
>((props, ref) => {
  const { badge, formatString, rule, fullName, ...rest } = props;
  const suffix =
    badge != null && badge !== "" ? (
      <SymbolBadge badge={badge} fullName={fullName} />
    ) : undefined;
  return (
    <FormattedText
      rule={"symbol"}
      formatString={"base"}
      {...rest}
      suffix={suffix}
      ref={ref}
    />
  );
});

SymbolBadgeText.displayName = "SymbolBadgeText";

/** Mobile-only: click-triggered popover; remounted via parent key when badge/fullName changes so open resets. */
const SymbolBadgeMobileTruncatedPopover = (props: {
  fullName: string;
  badgeEl: React.ReactElement;
}) => {
  const { fullName, badgeEl } = props;
  const [open, setOpen] = useState(false);

  return (
    <Popover
      arrow
      content={fullName}
      open={open}
      onOpenChange={setOpen}
      contentProps={{
        side: "top",
        align: "center",
        className:
          "oui-w-auto oui-border-0 oui-px-2 oui-py-1 oui-text-xs oui-text-base-contrast",
      }}
    >
      <span
        className="oui-inline-flex oui-cursor-pointer"
        onClick={(e) => {
          e.stopPropagation();
        }}
      >
        {badgeEl}
      </span>
    </Popover>
  );
};

export const SymbolBadge = (props: { badge?: string; fullName?: string }) => {
  const { badge, fullName } = props;
  const { isMobile } = useScreen();

  if (!badge) return null;

  const badgeEl = (
    <Badge color="neutral" size="xs" className="oui-ms-1">
      {badge}
    </Badge>
  );

  if (!fullName) return badgeEl;

  // Truncation format must stay in sync with useBadgeBySymbol (slice(0, 7) + "...")
  const isTruncated = badge.endsWith("...");

  if (isMobile) {
    if (!isTruncated) return badgeEl;

    return (
      <SymbolBadgeMobileTruncatedPopover
        key={`${badge}-${fullName}-${isMobile}`}
        fullName={fullName}
        badgeEl={badgeEl}
      />
    );
  }

  if (!isTruncated) return badgeEl;

  return (
    <Tooltip content={fullName}>
      <span className="oui-inline-flex oui-cursor-pointer">{badgeEl}</span>
    </Tooltip>
  );
};
