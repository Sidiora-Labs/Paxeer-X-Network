import { FC, useMemo, useState } from "react";
import { ActiveModifiers } from "react-day-picker";
import { format } from "date-fns";
import { cnBase } from "tailwind-variants";
import type { SizeType } from "../helpers/sizeType";
import { CalendarIcon } from "../icon/calendar";
import { CaretDownIcon } from "../icon/caretDown";
import { useLocale } from "../locale";
import { Popover } from "../popover/popover";
import { selectVariants } from "../select/selectPrimitive";
import { Calendar, CalendarProps } from "./date/calendar";

export type DatePickerProps = {
  onChange?: (date: Date) => void;
  placeholder?: string;
  value?: Date;
  dateFormat?: string;
  size?: SizeType;
  className?: string;
  children?: React.ReactNode;
} & CalendarProps;

const DatePicker: FC<DatePickerProps> = (props) => {
  const {
    placeholder,
    dateFormat,
    onChange,
    value,
    size,
    className,
    children,
    ...calendarProps
  } = props;

  const [locale] = useLocale("picker");

  const { trigger } = selectVariants({ size });

  const [open, setOpen] = useState(false);

  const formattedValue = useMemo(() => {
    if (typeof value === "undefined") {
      return placeholder ?? locale.selectDate;
    }
    return format(value, dateFormat ?? "yyyy/MM/dd");
  }, [value, placeholder, locale, dateFormat]);

  const onSelect = (
    day: Date | undefined,
    selectedDay: Date,
    activeModifiers: ActiveModifiers,
    e: MouseEvent,
  ) => {
    //@ts-ignore
    calendarProps.onSelect?.(day, selectedDay, activeModifiers, e);

    if (day) {
      onChange?.(day);
      setOpen(false);
    }
  };

  return (
    <Popover
      open={open}
      onOpenChange={setOpen}
      contentProps={{
        className: "oui-w-auto oui-p-0",
      }}
      content={
        <Calendar
          mode="single"
          selected={value}
          // @ts-ignore
          onSelect={onSelect}
          {...calendarProps}
        />
      }
    >
      {children ?? (
        <button
          className={trigger({
            className: cnBase("oui-datepicker-trigger oui-group", className),
          })}
        >
          <div className="oui-flex oui-items-center oui-gap-x-2">
            <span className="oui-datepicker-trigger-icon">
              <CalendarIcon
                size={14}
                className="oui-text-inherit"
                opacity={1}
              />
            </span>
            <span>{formattedValue}</span>
          </div>
          <CaretDownIcon
            size={12}
            className="oui-datepicker-trigger-arrow oui-text-inherit oui-transition-transform group-data-[state=closed]:oui-rotate-0 group-data-[state=open]:oui-rotate-180"
            opacity={1}
          />
        </button>
      )}
    </Popover>
  );
};

DatePicker.displayName = "DatePicker";

export { DatePicker };
