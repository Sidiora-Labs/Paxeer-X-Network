import React, { FC } from "react";
import { DateRange } from "react-day-picker";
import { cnBase } from "tailwind-variants";
import { Flex } from "../flex";
import {
  DateRangePicker,
  DateRangePickerProps,
} from "../pickers/dateRangePicker";
import { DatePicker, DatePickerProps } from "../pickers/datepicker";
import { Picker, PickerProps } from "../pickers/picker";
import { CombineSelect } from "../select/combine";
import {
  SelectWithOptions,
  type SelectWithOptionsProps,
} from "../select/withOptions";

type FilterType =
  | "select"
  | "input"
  | "date"
  | "range"
  | "custom"
  | "symbol"
  | "picker";

type DataFilterGeneral = {
  // initialValue?: any;
  name: string;
  type: FilterType;
};

type SelectFilter = {
  type: "select";
  isCombine?: boolean;
  // options: DataFilterOption[];
} & SelectWithOptionsProps;

// type DateFilterMode = "range" | "single";

type DateFilter = {
  type: "date";
  // range?: DataFilterDateRange;
  // mode?: DateFilterMode;
} & DatePickerProps;

type DateRangeFilter = {
  type: "range";
} & DateRangePickerProps;

type SymbolFilter = {
  type: "symbol";
};

type PickerFilter = {
  type: "picker";
} & PickerProps;

export type DataFilterItem = DataFilterGeneral &
  (SelectFilter | DateFilter | DateRangeFilter | SymbolFilter | PickerFilter);

export type DataFilterProps = {
  items: DataFilterItem[];
  onFilter: (filter: { name: string; value: any }) => void;
  className?: string;
  trailing?: React.ReactNode;
};

const FilterDatePicker = (props: DatePickerProps) => {
  return (
    <div>
      {/* @ts-ignore */}
      <DatePicker
        size="xs"
        {...props}
        mode={"single"}
        onChange={(date) => {
          console.log(date);
        }}
      />
    </div>
  );
};

const FilterDateRangePicker = (props: DateRangePickerProps) => {
  const { onChange, ...rest } = props;

  const onValueChange = (value: DateRange) => {
    // const from =value.from
    // console.log(value);

    if (typeof onChange === "function") {
      onChange(value);
    }
  };

  return (
    <div className={"oui-min-w-[180px]"}>
      <DateRangePicker size="xs" {...rest} onChange={onValueChange} />
    </div>
  );
};

export const DataFilterRenderer: FC<{
  type: FilterType;
  onChange?: (value: any) => void;
  [key: string]: any;
}> = (props) => {
  const { onChange, type, isCombine, ...rest } = props;

  switch (type) {
    case "select":
      return (
        <div className="oui-min-w-24">
          {isCombine ? (
            <CombineSelect
              size="xs"
              {...(rest as SelectWithOptionsProps)}
              onValueChange={onChange}
            />
          ) : (
            <SelectWithOptions
              size="xs"
              {...(rest as SelectWithOptionsProps)}
              onValueChange={onChange}
            />
          )}
        </div>
      );
    case "date":
      return <FilterDatePicker {...(rest as DatePickerProps)} />;
    case "range":
      return (
        <FilterDateRangePicker
          {...(rest as DateRangePickerProps)}
          onChange={onChange}
        />
      );
    case "symbol":
      return <div></div>;
    case "picker":
      return <Picker {...(rest as PickerProps)} onValueChange={onChange} />;
    case "input":
    default:
      return <div>No Component</div>;
  }
};

export const DataFilter: React.FC<DataFilterProps> = (props) => {
  return (
    <Flex
      justify={"start"}
      gapX={3}
      py={3}
      width={"100%"}
      className={cnBase("oui-data-filter-bar", props.className)}
    >
      {props.items.filter(Boolean).map((item, index: number) => {
        if (item.type === "date") {
          (item as DatePickerProps).mode = "range";
        }
        return (
          <DataFilterRenderer
            key={index}
            {...item}
            onChange={(value: any) => {
              // onChange(item.name ?? item.type, value);
              props.onFilter({ name: item.name ?? item.type, value });
            }}
          />
        );
      })}
      {props.trailing && (
        <div className="oui-flex oui-flex-1 oui-justify-end">
          {props.trailing}
        </div>
      )}
    </Flex>
  );
};
