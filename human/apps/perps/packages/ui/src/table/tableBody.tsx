import React, { FC, Fragment } from "react";
import { Row } from "@tanstack/react-table";
import { cnBase } from "tailwind-variants";
import { alignVariants } from "./className";
import { DataTableProps } from "./dataTable";
import { TableCell } from "./tableCell";
import { getColumnPinningProps } from "./utils/getColumnPinningProps";

type TableBodyProps<RecordType> = {
  className?: string;
  rows: Row<RecordType>[];
  justified?: boolean;
  showLeftShadow?: boolean;
  showRightShadow?: boolean;
  isRTL?: boolean;
  testId?: string;
} & Pick<
  DataTableProps<any>,
  "bordered" | "onRow" | "onCell" | "renderRowContainer" | "expandRowRender"
>;

export const TableBody: React.FC<TableBodyProps<any>> = (props) => {
  return (
    <tbody
      className={cnBase(
        "oui-table-tbody oui-relative",
        "oui-text-base-contrast-80",
        props.className,
      )}
      data-testid={props.testId}
    >
      {props.rows.map((row) => {
        const { className, onClick, ...rest } =
          typeof props.onRow === "function"
            ? props.onRow(row.original, row.index, row) || {}
            : {};

        const expandView = row.getIsExpanded() && (
          <tr className="oui-table-expand-tr oui-z-0 [&>td:first-child]:oui-ps-3 [&>td:last-child]:oui-pe-3">
            <td
              className="oui-table-expand-td"
              colSpan={row.getVisibleCells().length}
            >
              {props.expandRowRender?.(row, row.index)}
            </td>
          </tr>
        );

        const rowView = (
          <Fragment key={row.id}>
            <tr
              key={row.id}
              className={cnBase(
                "oui-table-tbody-tr oui-group oui-h-10",
                "[&>td:first-child]:oui-ps-3 [&>td:last-child]:oui-pe-3",
                typeof onClick === "function" && "oui-cursor-pointer",
                props.bordered && "oui-border-b oui-border-b-line-4",
                className,
              )}
              onClick={onClick}
              {...rest}
            >
              {row.getVisibleCells().map((cell) => {
                const column = cell.column;
                const { style: pinStyle, classNames: pinClassNames } =
                  getColumnPinningProps(column, !!props.isRTL);
                const { align, className: rowClassName } =
                  column.columnDef.meta || ({} as any);

                const {
                  style: cellStyle,
                  className: cellClassName,
                  children,
                  ...rest
                } = typeof props.onCell === "function"
                  ? props.onCell(cell.column, row.original, row.index, cell) ||
                    {}
                  : {};

                const cellView =
                  children !== undefined ? (
                    children
                  ) : (
                    <>
                      <TableCell cell={cell} />
                      <CellHover
                        selected={row.getIsSelected()}
                        isFirst={column.getIsFirstColumn()}
                        isLast={column.getIsLastColumn()}
                      />
                    </>
                  );

                return (
                  <td
                    key={cell.id}
                    style={{ ...pinStyle, ...cellStyle }}
                    className={cnBase(
                      "oui-table-tbody-td oui-relative",
                      "oui-px-1",
                      alignVariants({ align }),
                      // rowClassName,
                      typeof rowClassName === "function"
                        ? rowClassName(row.original)
                        : rowClassName,
                      pinClassNames.content,
                      props.showLeftShadow && pinClassNames.leftShadow,
                      props.showRightShadow && pinClassNames.rightShadow,
                      cellClassName,
                    )}
                    {...rest}
                  >
                    {cellView}
                  </td>
                );
              })}
            </tr>

            {expandView}
          </Fragment>
        );

        if (typeof props.renderRowContainer === "function") {
          return (
            <Fragment key={row.id}>
              {props.renderRowContainer(row.original, row.index, rowView, row)}
            </Fragment>
          );
        }

        return rowView;
      })}
    </tbody>
  );
};

type CellHoverProps = {
  selected: boolean;
  isFirst: boolean;
  isLast: boolean;
};

const CellHover: FC<CellHoverProps> = ({ selected, isFirst, isLast }) => {
  return (
    <div
      className={cnBase(
        "oui-absolute oui-start-0 oui-top-0 oui-z-[-1]",
        "oui-size-full",
        "group-hover:oui-bg-line-4",
        selected && "oui-bg-line-6 group-hover:oui-bg-line-6",
        isFirst && "oui-rounded-l",
        isLast && "oui-rounded-r",
      )}
    />
  );
};
