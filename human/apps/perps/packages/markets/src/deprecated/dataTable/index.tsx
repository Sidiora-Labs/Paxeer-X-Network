import React, { FC, ReactNode, CSSProperties, useMemo } from "react";
import { cn, InjectableEmptyDataState, SortOrder } from "@paxeer-network/ui";
import { useSort } from "./useSort";

export type Column = {
  dataIndex: string;
  title: string;
  render?: (value: any, row: any) => ReactNode;
  className?: string;
  width?: CSSProperties["width"];
  align?: "left" | "right" | "center";
  onSort?: boolean | ((r1: any, r2: any, sortOrder: SortOrder) => number);
};

export type TableProps = {
  columns: Column[];
  dataSource: any[];
  emptyView?: ReactNode;
  className?: string;
  classNames?: {
    root?: string;
    header?: string;
    body?: string;
    footer?: string;
  };
  generatedRowKey?: (record: any, index: number) => string;
  onRow?: (record: any, index: number) => any;
  loading?: boolean;
  onSort?: (options?: { sortKey: string; sort: SortOrder }) => void;
  initialSort?: { sortKey: string; sort: SortOrder };
};

function alignClassName(align?: Column["align"]) {
  return cn(
    align === "left" && "oui-justify-start",
    align === "right" && "oui-justify-end",
    align === "center" && "oui-justify-center",
  );
}

const DataTable: FC<TableProps> = (props) => {
  const { columns, dataSource, className, onRow, classNames } = props;

  const { onSort, renderSortIndicator } = useSort({
    onSort: props.onSort,
    initialSort: props.initialSort,
  });

  return (
    <div
      className={cn(
        "oui-h-full oui-overflow-hidden",
        className,
        classNames?.root,
      )}
    >
      <div
        className={cn(
          "oui-flex oui-mx-3",
          "oui-text-xs oui-text-base-contrast-36",
          classNames?.header,
        )}
      >
        {columns.map((column, index) => {
          const sortable = !!column.onSort;
          return (
            <div
              key={column.dataIndex}
              style={{ width: column.width }}
              className={cn(
                "oui-flex oui-items-center oui-gap-x-1",
                "oui-break-normal oui-whitespace-nowrap",
                "oui-basis-[100%]",
                sortable && "oui-cursor-pointer",
                alignClassName(column.align),
                column.className,
              )}
              onClick={(e) => {
                e.stopPropagation();
                if (sortable) {
                  onSort(column.dataIndex);
                }
              }}
            >
              <span>{column.title}</span>
              {sortable && renderSortIndicator(column.dataIndex)}
            </div>
          );
        })}
      </div>

      <div
        className={cn(
          "oui-relative oui-h-full oui-overflow-y-auto oui-custom-scrollbar",
          "oui-text-2xs oui-text-base-contrast-80",
          classNames?.body,
        )}
      >
        {dataSource?.length
          ? dataSource.map((row, index) => {
              const key =
                typeof props.generatedRowKey === "function"
                  ? props.generatedRowKey(row, index)
                  : `${index}`;
              return (
                <TableItem
                  key={key}
                  row={row}
                  index={index}
                  columns={columns}
                  onRow={onRow}
                />
              );
            })
          : props.emptyView || (
              <div className="oui-w-full oui-h-full oui-flex oui-justify-center oui-items-center oui-mt-[-30px]">
                <InjectableEmptyDataState />
              </div>
            )}
      </div>
    </div>
  );
};

type TableItemProps = Pick<TableProps, "columns" | "onRow"> & {
  row: any;
  index: number;
  className?: string;
};

const TableItem: React.FC<TableItemProps> = (props) => {
  const { row, index, columns, onRow } = props;

  const rowAttrs = useMemo(() => {
    if (typeof onRow === "function") {
      return onRow(row, index);
    }
    return {};
  }, [row, index, onRow]);

  const { className: rowClassName, ...rest } = rowAttrs;

  return (
    <div
      key={row.id}
      className={cn(
        "oui-group",
        "oui-flex oui-justify-center oui-items-center",
        "oui-mx-1 oui-px-2",
        "hover:oui-bg-base-7 oui-rounded-md",
        props.className,
        rowClassName,
      )}
      {...rest}
    >
      {columns.map((column, idx) => {
        const value = row[column.dataIndex];
        return (
          <div
            key={column.dataIndex}
            style={{ width: column.width }}
            className={cn(
              "oui-flex oui-items-center",
              "oui-basis-[100%]",
              alignClassName(column.align),
              column.className,
            )}
          >
            {column.render ? column.render(value, row) : value}
          </div>
        );
      })}
    </div>
  );
};

export default DataTable;
