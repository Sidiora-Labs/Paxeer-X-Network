import { useEffect, useMemo, useRef } from "react";
import { DataTableProps } from "../dataTable";

export function useShowHeader(
  params: Pick<DataTableProps<any>, "dataSource" | "loading">
) {
  const { dataSource, loading } = params;

  const initShowHeader = useRef(false);

  const showHeader = useMemo(() => {
    if (loading && !dataSource?.length) {
      return initShowHeader.current;
    }

    return !!dataSource?.length;
  }, [loading, dataSource]);

  useEffect(() => {
    if (showHeader) {
      initShowHeader.current = true;
    }
  }, [showHeader]);

  return showHeader;
}
