import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { PaginationMeta } from "../type";

export const usePagination = (initial?: {
  page?: number;
  pageSize?: number;
  resetPageWhenPageSizeChange?: boolean;
}) => {
  const dataTotal = useRef(0);
  const [page, setPage] = useState<number>(initial?.page ?? 1);
  const [pageSize, _setPageSize] = useState<number>(initial?.pageSize ?? 10);

  const setPageSize = useCallback(
    (size: number) => {
      _setPageSize(size);
      // check page > page total
      if (dataTotal.current > 0) {
        const totalPage = Math.ceil(dataTotal.current / size);
        if (page > totalPage) {
          setPage(totalPage);
        }
      }
    },
    [page],
  );

  /**
   * helper function to parse meta data,
   * the format of meta data is same as the response from the API
   */
  const parseMeta = (meta?: {
    total: number;
    current_page: number;
    records_per_page: number;
  }) => {
    const count = meta?.total ?? dataTotal.current;
    dataTotal.current = count;
    const size = meta?.records_per_page ?? pageSize;
    const pageTotal = count ? Math.ceil(count / size) : 0;
    const curPage = Math.min(meta?.current_page ?? page, pageTotal);
    return {
      count,
      page: curPage,
      pageSize: size,
      pageTotal: pageTotal,
    };
  };

  /** server pagination */
  const parsePagination = useCallback(
    (meta?: {
      total: number;
      current_page: number;
      records_per_page: number;
    }): PaginationMeta => {
      return {
        ...parseMeta(meta),
        onPageChange: setPage,
        onPageSizeChange: setPageSize,
      };
    },
    [page, pageSize],
  );

  /** manual pagination */
  const pagination = useMemo<PaginationMeta>(
    () => ({
      page,
      pageSize,
      onPageChange: setPage,
      onPageSizeChange: setPageSize,
    }),
    [parsePagination],
  );

  useEffect(() => {
    // reset page when page size change
    if (initial?.resetPageWhenPageSizeChange !== false) {
      setPage(1);
    }
  }, [pageSize, initial?.resetPageWhenPageSizeChange]);

  return {
    page,
    pageSize,
    setPage,
    setPageSize,
    parseMeta,
    pagination,
    parsePagination,
  };
};
