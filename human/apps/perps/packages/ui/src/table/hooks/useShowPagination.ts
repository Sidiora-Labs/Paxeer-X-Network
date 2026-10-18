import { useEffect, useMemo, useRef } from "react";
import { DataTableProps } from "../dataTable";
import { Row } from "@tanstack/react-table";

export function useShowPagination(
  params: Pick<DataTableProps<any>, "dataSource" | "loading" | "pagination"> & {
    rows: Row<any>[];
  }
) {
  const { loading, dataSource, pagination, rows } = params;

  const initShowPagination = useRef(false);

  const showPagination = useMemo(() => {
    if (loading && !dataSource?.length) {
      return initShowPagination.current;
    }

    if (pagination && dataSource?.length && rows?.length) {
      return true;
    }

    return false;
  }, [loading, pagination, dataSource, rows]);

  useEffect(() => {
    if (showPagination) {
      initShowPagination.current = true;
    }
  }, [showPagination]);

  return showPagination;
}
