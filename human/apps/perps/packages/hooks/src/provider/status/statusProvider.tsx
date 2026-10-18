import { PropsWithChildren, useMemo } from "react";
import { useWsStatus } from "../../useWsStatus";
import { StatusContext, StatusContextState } from "./statusContext";

export const StatusProvider: React.FC<PropsWithChildren> = (props) => {
  const wsStatus = useWsStatus();

  const memoizedValue = useMemo<StatusContextState>(() => {
    return { ws: wsStatus };
  }, [wsStatus]);

  return (
    <StatusContext.Provider value={memoizedValue}>
      {props.children}
    </StatusContext.Provider>
  );
};
