import { ReactNode } from "react";
import {
  Cell,
  Row,
  type Column as TanstackColumn,
} from "@tanstack/react-table";
import { FormattedTextProps, TextRule } from "../typography/formatted";
import { NumeralProps, type NumeralRule } from "../typography/numeral";

export {
  type Column as TanstackColumn,
  type Table,
} from "@tanstack/react-table";

export type ColumnFixed = "left" | "right" | "start" | "end";

export type SortOrder = "asc" | "desc";

export type TableSort = { sortKey: string; sort: SortOrder };

// Multi-field display support
export type MultiFieldSort = {
  sortKey: string;
  sort: SortOrder;
};

export type TableCellContext = Cell<any, any>;

type PlainText = number | string | null | undefined;

export type TableCellFormatter<T> =
  | string
  | ((value: any, record: T, index: number) => any);

export type TableCellRenderer<T> =
  | string
  | ((
      value: any,
      record: T,
      index: number,
      context: TableCellContext,
    ) => React.ReactNode);

export type TableCellPlainTextRenderer<T> = (
  value: any,
  record: T,
  index: number,
  context: TableCellContext,
) => PlainText;

export type MultiSortField = {
  sortKey: string;
  label?: string;
  className?: string;
};

export type Column<RecordType extends unknown = any> = {
  type?: "data" | "action" | "group";
  title?: ReactNode;
  /**
   * when title is ReactElement, download feature need this field to render plant text
   * */
  plantTextTitle?: PlainText;
  hint?: ReactNode;
  hintClassName?: string;
  width?: number;
  fixed?: ColumnFixed;
  dataIndex: string;
  className?: string | ((record: RecordType, index: number) => string);
  align?: "left" | "center" | "right";
  onSort?:
    | boolean
    | ((r1: RecordType, r2: RecordType, sortOrder?: SortOrder) => number);
  // Multi-field sorting support
  multiSort?: {
    fields: MultiSortField[];
    initialSort?: MultiFieldSort[]; // UI display only - shows initial sort indicators
    onSort?: (fieldKey: string, sortOrder?: SortOrder) => void; // Handler for field-specific sorting
  };
  formatter?: TableCellFormatter<RecordType>;
  render?: TableCellRenderer<RecordType>;
  /**
   * when render return ReactElement, download feature need this field to render plant text
   * */
  renderPlantText?: TableCellPlainTextRenderer<RecordType>;
  getKey?: (record: RecordType, index: number) => string;

  /**
   * text rule for formatted text, if provided, the text will be rendered as formatted text component;
   */
  rule?: TextRule | NumeralRule;
  numeralProps?:
    | Omit<NumeralProps, "children" | "as" | "rule">
    | ((
        value: any,
        record: RecordType,
        index: number,
      ) => Omit<NumeralProps, "children" | "as" | "rule">);
  /**
   * text props for formatted text
   */
  textProps?:
    | Omit<FormattedTextProps, "children" | "as" | "rule">
    | ((
        value: any,
        record: RecordType,
        index: number,
      ) => Omit<FormattedTextProps, "children" | "as" | "rule">);
};

export type PaginationMeta = {
  count?: number;
  page: number;
  pageSize: number;
  pageTotal?: number;
  onPageChange?: (page: number) => void;
  onPageSizeChange?: (pageSize: number) => void;
  generatePageNumbers?: (
    currentPage: number,
    totalPages: number,
  ) => (number | string)[];
  hideRowsPerPage?: boolean;
};

export type DataTableClassNames = {
  root?: string;
  header?: string;
  body?: string;
  footer?: string;
  pagination?: string;
  scroll?: string;
  empty?: string;
};
