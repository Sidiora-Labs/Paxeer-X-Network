import React, { FC, PropsWithChildren, ReactNode, useMemo } from "react";
import { ChevronDownIcon, cn } from "../..";
import {
  CollapsibleContent,
  Collapsible,
  CollapsibleTrigger,
} from "../collapsible";
import { useCollapseContext } from "./collapseContext";

interface Props {
  header: ReactNode;
  headerClassName?: string;
  itemKey: string;
  disabled?: boolean;
}

export const Panel: FC<PropsWithChildren<Props>> = (props) => {
  const { activeKey, setActiveKey } = useCollapseContext();
  const { header, headerClassName, itemKey, disabled } = props;
  const headerNode = useMemo<React.ReactNode>(() => {
    if (typeof header === "string") {
      return (
        <div
          className={cn(
            "oui-border-divider flex items-center oui-group oui-cursor-pointer oui-border-b oui-py-2",
            headerClassName,
          )}
        >
          <div className="oui-flex-1 group-data-[state=open]:oui-font-semibold">
            {header}
          </div>
          <ChevronDownIcon
            size={16}
            className="oui-transition-transform group-data-[state=open]:oui-rotate-180"
          />
        </div>
      );
    }
    return header;
  }, [header, headerClassName]);

  return (
    <Collapsible
      disabled={disabled}
      open={itemKey === activeKey}
      onOpenChange={(open) => {
        if (open) {
          setActiveKey(itemKey);
        } else {
          setActiveKey("");
        }
      }}
    >
      <CollapsibleTrigger asChild>{headerNode}</CollapsibleTrigger>
      <CollapsibleContent className="oui-py-3">
        {props.children}
      </CollapsibleContent>
    </Collapsible>
  );
};
