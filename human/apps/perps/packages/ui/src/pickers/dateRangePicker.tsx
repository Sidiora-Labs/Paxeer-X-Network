import { FC, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { DateRange, DayPickerRangeProps } from "react-day-picker";
import { format } from "date-fns";
import type { SizeType } from "../helpers/sizeType";
import { CalendarIcon } from "../icon/calendar";
import { CaretDownIcon } from "../icon/caretDown";
import { useLocale } from "../locale";
import { Popover } from "../popover/popover";
import { selectVariants } from "../select/selectPrimitive";
import { isWithinCalendarMonths, isWithinDays } from "./calendarRange";
import { Calendar, CalendarProps } from "./date/calendar";

export type DateRangePickerProps = {
  onChange?: (date: DateRange) => void;
  // selected: Date;
  placeholder?: string;
  value?: DateRange;
  initialValue?: DateRange;
  dateFormat?: string;
  size?: SizeType;
  className?: string;
  formatString?: string;
  /** Maximum span between the selected dates, measured in calendar months. */
  maxCalendarMonths?: number;
  /** Maximum span between the selected dates, measured in days, inclusive. */
  maxDays?: number;
} & Omit<DayPickerRangeProps, "mode">;

const DEFAULT_DATE_FORMAT = "yyyy/MM/dd";

const DateRangePicker: FC<DateRangePickerProps> = (props) => {
  const {
    placeholder,
    dateFormat,
    onChange,
    value,
    initialValue,
    size,
    className,
    formatString = DEFAULT_DATE_FORMAT,
    maxCalendarMonths,
    maxDays,
    ...calendarProps
  } = props;
  const [locale] = useLocale("picker");
  const [open, setOpen] = useState(false);
  const [dateRange, setDateRange] = useState<DateRange | null>(
    value || initialValue || null,
  );

  const [isMobileView, setIsMobileView] = useState<boolean>(false);

  const update = useDebouncedCallback((width: number) => {
    setIsMobileView(width < 1024);
  }, 100);

  // Effect hook to listen to window resize events
  useEffect(() => {
    const handleResize = () => {
      update(window.innerWidth);
    };

    setIsMobileView(window.innerWidth < 1024);
    window.addEventListener("resize", handleResize);

    // Cleanup event listener on component unmount
    return () => {
      window.removeEventListener("resize", handleResize);
    };
  }, []);

  useEffect(() => {
    if (
      value?.from &&
      value?.to &&
      dateRange?.from &&
      dateRange?.to &&
      !areDatesEqual(value as any, dateRange as any)
    )
      setDateRange(value);
  }, [value]);

  const { trigger } = selectVariants({ size, className });

  const formattedValue = useMemo(() => {
    // console.log("dateRange", dateRange);
    if (!dateRange || !dateRange.from || !dateRange.to) {
      return placeholder ?? locale.selectDate;
    }
    const arr = [];
    if (dateRange.from) arr.push(format(dateRange.from, formatString));
    if (dateRange.to) arr.push(format(dateRange.to, formatString));

    return `${arr.join(" - ")}`;
  }, [dateRange, placeholder, locale]);

  const isWithinMaxSpan = (from: Date, to: Date) => {
    if (
      maxCalendarMonths &&
      !isWithinCalendarMonths(from, to, maxCalendarMonths)
    ) {
      return false;
    }
    if (maxDays && !isWithinDays(from, to, maxDays)) {
      return false;
    }
    return true;
  };

  const onOpenChange = (nextOpen: boolean) => {
    if (!nextOpen && dateRange?.from) {
      const nextRange = {
        from: dateRange.from,
        to: dateRange.to ?? dateRange.from,
      };
      if (isWithinMaxSpan(nextRange.from, nextRange.to)) {
        setDateRange(nextRange);
        onChange?.(nextRange);
      }
    }
    setOpen(nextOpen);
  };

  const onSelected = (range: DateRange, date: Date) => {
    if (dateRange?.from && dateRange?.to) {
      setDateRange({
        from: date,
      });
    } else {
      if (range.from && range.to && !isWithinMaxSpan(range.from, range.to)) {
        return;
      }
      setDateRange(range);
    }
  };

  const disabledMatchers = calendarProps.disabled
    ? Array.isArray(calendarProps.disabled)
      ? calendarProps.disabled
      : [calendarProps.disabled]
    : [];
  const disabled =
    (maxCalendarMonths || maxDays) && dateRange?.from && !dateRange.to
      ? [
          ...disabledMatchers,
          (date: Date) => !isWithinMaxSpan(dateRange.from!, date),
        ]
      : calendarProps.disabled;

  return (
    <Popover
      open={open}
      onOpenChange={onOpenChange}
      contentProps={{
        className: "oui-w-auto oui-p-0",
        align: "start",
      }}
      content={
        <Calendar
          numberOfMonths={isMobileView ? 1 : 2}
          {...calendarProps}
          disabled={disabled}
          mode={"range"}
          // @ts-ignore
          selected={dateRange}
          // @ts-ignore
          onSelect={onSelected}
        />
      }
    >
      <button
        className={trigger({
          className: "oui-datepicker-trigger oui-group",
        })}
      >
        <span className="oui-datepicker-trigger-icon">
          <CalendarIcon size={14} className="oui-text-inherit" opacity={1} />
        </span>
        <span>{formattedValue}</span>
        <CaretDownIcon
          size={12}
          className="oui-datepicker-trigger-arrow oui-text-inherit oui-transition-transform group-data-[state=open]:oui-rotate-180 group-data-[state=closed]:oui-rotate-0"
          opacity={1}
        />
      </button>
    </Popover>
  );
};

DateRangePicker.displayName = "DateRangePicker";

export { DateRangePicker };

function useDebouncedCallback(callback: any, delay: number) {
  const timeoutRef = useRef<any | null>(null);

  const debouncedCallback = useCallback(
    (args: any) => {
      if (timeoutRef.current) {
        clearTimeout(timeoutRef.current);
      }

      timeoutRef.current = setTimeout(() => {
        callback(args);
      }, delay);
    },
    [callback, delay],
  );
  return debouncedCallback;
}

function areDatesEqual(
  date1: { from: Date; to: Date },
  date2: { from: Date; to: Date },
): boolean {
  const extractDateParts = (date: Date) => ({
    year: date.getFullYear(),
    month: date.getMonth(),
    day: date.getDate(),
  });

  const from1 = extractDateParts(date1.from);
  const to1 = extractDateParts(date1.to);
  const from2 = extractDateParts(date2.from);
  const to2 = extractDateParts(date2.to);

  return (
    from1.year === from2.year &&
    from1.month === from2.month &&
    from1.day === from2.day &&
    to1.year === to2.year &&
    to1.month === to2.month &&
    to1.day === to2.day
  );
}
