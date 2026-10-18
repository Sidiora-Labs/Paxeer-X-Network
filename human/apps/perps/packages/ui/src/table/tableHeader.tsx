import { FC } from "react";
import {
  HeaderGroup,
  SortDirection,
  SortingState,
} from "@tanstack/react-table";
import { cnBase } from "tailwind-variants";
import { alignVariants } from "./className";
import { DataTableProps } from "./dataTable";
import { AscendingIcon, DescendingIcon, SortingIcon } from "./icons";
import { MultiSortHeader } from "./multiSortHeader";
import { getColumnPinningProps } from "./utils/getColumnPinningProps";

type TableHeaderProps = {
  className?: string;
  headerGroups: HeaderGroup<any>[];
  showLeftShadow?: boolean;
  showRightShadow?: boolean;
  isRTL?: boolean;
  sorting?: SortingState;
} & Pick<DataTableProps<any>, "bordered">;

export const TableHeader: FC<TableHeaderProps> = (props) => {
  return (
    <thead
      className={cnBase(
        "oui-table-thead",
        "oui-sticky oui-top-0 oui-z-[2]",
        "oui-bg-[var(--oui-table-background-color)]",
        "oui-text-base-contrast-36",
      )}
    >
      {props.headerGroups.map((headerGroup) => (
        <tr
          key={headerGroup.id}
          className={cnBase(
            "oui-table-thead-tr",
            "oui-h-10",
            "[&>th:first-child]:oui-ps-3 [&>th:last-child]:oui-pe-3",
            props.className,
          )}
        >
          {headerGroup.headers.map((header) => {
            const column = header.column;
            const {
              align,
              className: rowClassName,
              title,
            } = column.columnDef.meta || ({} as any);

            const { style: pinStyle, classNames: pinClassNames } =
              getColumnPinningProps(column, !!props.isRTL);

            const canSort = column.getCanSort();
            const isSorted = column.getIsSorted();

            return (
              <th
                key={header.id}
                style={pinStyle}
                className={cnBase(
                  "oui-table-thead-th",
                  "oui-whitespace-nowrap",
                  "oui-px-1",
                  props.bordered &&
                    "after:oui-absolute after:oui-start-0 after:oui-top-0 after:oui-z-[-1] after:oui-block after:oui-size-full after:oui-border-b after:oui-border-line",
                  // use border-b will show bottom element content
                  // props.bordered && "oui-border-b oui-border-line"
                  alignVariants({ align }),
                  rowClassName,
                  pinClassNames.content,
                  props.showLeftShadow && pinClassNames.leftShadow,
                  props.showRightShadow && pinClassNames.rightShadow,
                )}
              >
                {header.isPlaceholder ? null : (column.columnDef.meta as any)
                    ?.multiSort ? (
                  <MultiSortHeader
                    column={column.columnDef.meta as any}
                    sorting={props.sorting}
                  />
                ) : (
                  <div
                    className={cnBase(
                      "oui-inline-flex oui-items-center oui-gap-x-1",
                      canSort &&
                        "oui-cursor-pointer oui-select-none hover:oui-text-base-contrast-80",
                    )}
                    onClick={column.getToggleSortingHandler()}
                  >
                    {title}
                    {canSort && (
                      <SortIndicator isSorted={isSorted as SortDirection} />
                    )}
                  </div>
                )}
              </th>
            );
          })}
        </tr>
      ))}
    </thead>
  );
};

const SortIndicator: FC<{ isSorted: SortDirection }> = ({ isSorted }) => {
  return (
    {
      asc: <AscendingIcon />,
      desc: <DescendingIcon />,
    }[isSorted] || <SortingIcon />
  );
};
