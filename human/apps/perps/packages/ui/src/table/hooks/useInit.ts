import { useEffect, useState } from "react";
import { DataTableProps } from "../dataTable";

export function useInit(
  params: Pick<
    DataTableProps<any>,
    "dataSource" | "loading" | "ignoreLoadingCheck"
  >
) {
  const { dataSource, loading, ignoreLoadingCheck } = params;
  const [initialized, setInitialized] = useState(false);

  useEffect(() => {
    if (initialized) return;

    if (ignoreLoadingCheck || loading || Array.isArray(dataSource)) {
      setInitialized(true);
    }
  }, [loading, ignoreLoadingCheck, dataSource, initialized]);

  return initialized;
}
