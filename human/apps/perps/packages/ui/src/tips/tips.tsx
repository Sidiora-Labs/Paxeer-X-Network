import React from "react";
import { cnBase } from "tailwind-variants";
import { useScreen } from "../hooks";
import { InfoCircleIcon } from "../icon";
import { modal } from "../modal";
import { Tooltip } from "../tooltip";

const stopPropagation = (event: React.MouseEvent | React.PointerEvent) => {
  event.stopPropagation();
};

export type TipsProps = {
  /** Content shown in desktop Tooltip and mobile dialog body (same for both). */
  content: React.ReactNode;
  /** Title for mobile modal.alert; defaults to "Tips" when not provided. */
  title?: string;
  /** Trigger element; defaults to InfoCircleIcon when not provided. */
  trigger?: React.ReactNode;
  /** Optional className for the wrapper (button on mobile, or Tooltip trigger on desktop). */
  className?: string;
  classNamss?:{trigger?:string};
  classNames?: {
    trigger?: string;
  };
};

export const Tips: React.FC<TipsProps> = ({
  content,
  title = "Tips",
  trigger,
  className,
  classNames,
}) => {
  const { isMobile } = useScreen();

  const triggerElement = trigger || (
    <InfoCircleIcon
      opacity={1}
      className={cnBase(
        "oui-size-3 oui-shrink-0 oui-cursor-pointer oui-text-base-contrast-36",
        classNames?.trigger,
      )}
    />
  );

  if (isMobile) {
    const handleClick = (event: React.MouseEvent<HTMLButtonElement>) => {
      event.stopPropagation();
      modal.alert({
        title,
        message: content,
      });
    };

    return (
      <button
        type="button"
        className={className}
        onClick={handleClick}
        onMouseDown={stopPropagation}
        onPointerDown={stopPropagation}
      >
        {triggerElement}
      </button>
    );
  }
  return (
    <Tooltip
      className="oui-bg-base-6"
      arrow={{ className: "oui-fill-base-6" }}
      content={<div className="oui-max-w-72">{content}</div>}
    >
      {triggerElement}
    </Tooltip>
  );
};

Tips.displayName = "Tips";
