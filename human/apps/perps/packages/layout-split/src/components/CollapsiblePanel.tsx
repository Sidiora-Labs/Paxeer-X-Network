import React, { useMemo, type FC, type SVGProps } from "react";
import { cn } from "@paxeer-network/ui";
import { Flex, Text } from "@paxeer-network/ui";

export const ExpandIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M6.326 8.826a.84.84 0 0 0-.6.234L2.16 12.627v-2.135H.492v4.167c0 .46.373.833.834.833h4.166v-1.667H3.357l3.567-3.567a.857.857 0 0 0 0-1.198.84.84 0 0 0-.598-.234M10.502.492V2.16h2.135L9.07 5.726a.857.857 0 0 0 0 1.199.86.86 0 0 0 1.197 0l3.568-3.568v2.135h1.667V1.326a.834.834 0 0 0-.834-.834z" />
  </svg>
);

export const CollapseIcon: FC<SVGProps<SVGSVGElement>> = (props) => (
  <svg
    width="16"
    height="16"
    viewBox="0 0 16 16"
    fill="currentColor"
    xmlns="http://www.w3.org/2000/svg"
    {...props}
  >
    <path d="M14.668.492a.85.85 0 0 0-.599.234l-3.567 3.568V2.159H8.835v4.167c0 .46.373.833.833.833h4.167V5.492H11.7l3.569-3.567a.86.86 0 0 0 0-1.199.85.85 0 0 0-.6-.234m-12.5 8.334v1.666h2.135L.736 14.06a.86.86 0 0 0 0 1.198.86.86 0 0 0 1.198 0l3.568-3.567v2.134h1.666V9.66a.834.834 0 0 0-.833-.833z" />
  </svg>
);

export interface CollapsiblePanelProps {
  /** Panel title */
  title?: React.ReactNode;
  /** Whether the panel can be collapsed */
  collapsible?: boolean;
  /** Current collapsed state */
  collapsed?: boolean;
  /** Callback to toggle collapse state */
  onToggle?: () => void;
  /** Minimum size (used when collapsed) */
  minSize?: string;
  /** Maximum size (used when expanded) */
  maxSize?: string;
  /** Panel content */
  children?: React.ReactNode;
  /** Additional className for wrapper */
  className?: string;
  /** Additional style for wrapper */
  style?: React.CSSProperties;
}

/**
 * A collapsible panel component with title and collapse/expand functionality.
 */
export const CollapsiblePanel: React.FC<CollapsiblePanelProps> = ({
  title,
  collapsible,
  collapsed,
  onToggle,
  minSize,
  maxSize,
  children,
  className,
  style,
}) => {
  // Calculate width based on collapsed state
  const computedStyle = Object.assign(
    {},
    style,
    typeof collapsible !== "undefined"
      ? collapsed
        ? { width: minSize }
        : { width: maxSize }
      : {},
  );

  const bodyClassName = useMemo(() => {
    return typeof !!title ? "oui-h-[calc(100%_-_40px)]" : "oui-h-full";
  }, [title]);

  const renderCollapsibleHeader = () => {
    // if (typeof collapsible !== "undefined" && !collapsible) return null;

    return (
      <Flex
        className={cn(
          "oui-text-base-contrast-36",
          // collapsed ? "oui-absolute oui-end-[-20px] oui-z-50" : "oui-relative",
        )}
        justify={collapsed ? "center" : "between"}
        width="100%"
        px={3}
        pt={3}
      >
        {!collapsed && title && (
          <Text size="base" intensity={80}>
            {title}
          </Text>
        )}
        <button
          onClick={onToggle}
          className={cn("oui-cursor-pointer hover:oui-text-base-contrast-80")}
        >
          {collapsed ? (
            <ExpandIcon className="oui-text-base-contrast-36" />
          ) : (
            <CollapseIcon className="oui-text-base-contrast-36" />
          )}
        </button>
      </Flex>
    );
  };

  return (
    <div
      className={cn(
        "oui-flex oui-size-full oui-flex-col oui-gap-y-5 oui-overflow-hidden oui-rounded-2xl oui-bg-base-9",
        className,
      )}
      style={computedStyle}
    >
      {renderCollapsibleHeader()}

      <div className={cn("oui-collapsible-content", bodyClassName)}>
        {React.cloneElement(children as React.ReactElement, {
          collapsed,
          // panelSize: "small",
        })}
      </div>
    </div>
  );
};
