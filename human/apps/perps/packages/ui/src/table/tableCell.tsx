import React, { ReactNode } from "react";
import { Cell, flexRender } from "@tanstack/react-table";
import { FormattedText, isTextRule } from "../typography/formatted";
import { isNumeralRule, Numeral } from "../typography/numeral";
import { Column } from "./type";

type TableCellProps = {
  cell: Cell<any, any>;
};

export const TableCell: React.FC<TableCellProps> = (props) => {
  const { cell } = props;

  const { original: record, index } = cell.row;
  const { formatter, render, rule, textProps, numeralProps } = (cell.column
    .columnDef.meta || {}) as Column;

  let value = cell.getValue();

  if (typeof formatter === "function") {
    value = formatter(value, record, index);
  }

  if (typeof render === "function") {
    return render(value, record, index, cell);
  }

  if (typeof rule !== "undefined") {
    if (isTextRule(rule)) {
      const _textProps =
        typeof textProps === "function"
          ? textProps(value, record, index)
          : textProps;
      return (
        <FormattedText rule={rule} {..._textProps}>
          {value}
        </FormattedText>
      );
    }

    if (isNumeralRule(rule)) {
      const _numeralProps =
        typeof numeralProps === "function"
          ? numeralProps(value, record, index)
          : numeralProps;

      return (
        <Numeral rule={rule} {..._numeralProps}>
          {value}
        </Numeral>
      );
    }
  }

  return flexRender(cell.column.columnDef.cell, cell.getContext()) as ReactNode;
};
