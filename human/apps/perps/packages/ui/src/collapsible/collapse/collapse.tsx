import React, { useMemo, useState } from "react";
import { CollapseContext } from "./collapseContext";
import type { CollapseContextState } from "./collapseContext";
import { Panel } from "./panel";

const CollapseRoot: React.FC<
  React.PropsWithChildren<{ activeKey?: string }>
> = (props) => {
  const { activeKey = "", children } = props;
  const [internalActiveKey, setInternalActiveKey] = useState<string>(activeKey);
  const memoizedValue = useMemo<CollapseContextState>(() => {
    return {
      activeKey: internalActiveKey,
      setActiveKey: setInternalActiveKey,
    };
  }, [internalActiveKey, setInternalActiveKey]);
  return (
    <CollapseContext.Provider value={memoizedValue}>
      {children}
    </CollapseContext.Provider>
  );
};

type Collapse = typeof CollapseRoot & {
  panel: typeof Panel;
};

const Collapse = CollapseRoot as Collapse;

Collapse.panel = Panel;

export default Collapse;
