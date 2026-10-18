import { CSSProperties } from "react";
import { cnBase } from "tailwind-variants";
import { TanstackColumn } from "../type";

export function getColumnPinningProps(
  column: TanstackColumn<any>,
  isRTL: boolean,
) {
  const isPinned = column.getIsPinned();
  const isLastLeftPinnedColumn =
    isPinned === "left" && column.getIsLastColumn("left");
  const isFirstRightPinnedColumn =
    isPinned === "right" && column.getIsFirstColumn("right");

  const style: CSSProperties = {
    // Keep logical inset here. TanStack pinning still reports left/right
    // groups, but sticky offsets must follow the scroll container direction;
    // physical left/right breaks fixed columns in RTL tables.
    insetInlineStart:
      isPinned === "left" ? `${column.getStart("left")}px` : undefined,
    insetInlineEnd:
      isPinned === "right" ? `${column.getAfter("right")}px` : undefined,
    width: column.getSize(),
  };

  const contentCls = cnBase(
    isPinned ? "oui-sticky" : "oui-relative",
    isPinned ? "oui-z-[1]" : "oui-z-0",
    isPinned && "oui-bg-[var(--oui-table-background-color)]",
  );

  const shadowCls = cnBase(
    "before:oui-block before:oui-absolute",
    "before:oui-w-[32px] before:oui-h-full",
    "before:oui-top-0 before:oui-z-[-1]",
    "before:oui-bg-[linear-gradient(90deg,rgba(var(--oui-color-base-10)_/_0.80)_0%,rgba(var(--oui-color-base-10)_/_0.36)_65%,rgba(var(--oui-color-base-10)_/_0.00)_100%)]",
  );

  const leftShadow =
    isLastLeftPinnedColumn &&
    cnBase(
      shadowCls,
      // The shadow sits on the logical outer edge of the pinned group.
      "before:oui-end-[-32px]",
      isRTL && "before:oui-rotate-180",
    );

  const rightShadow =
    isFirstRightPinnedColumn &&
    cnBase(
      shadowCls,
      // Do not replace with physical left/right; this mirrors with RTL pinning.
      "before:oui-start-[-32px]",
      !isRTL && "before:oui-rotate-180",
    );

  return {
    style,
    classNames: {
      content: contentCls,
      leftShadow,
      rightShadow,
    },
  };
}
