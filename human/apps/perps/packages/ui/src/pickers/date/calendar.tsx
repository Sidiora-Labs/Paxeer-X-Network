import { DayPicker } from "react-day-picker";
import { cnBase } from "tailwind-variants";
import { ChevronLeftIcon, ChevronRightIcon } from "../../icon";
import { useLocale } from "../../locale";

export type CalendarProps = React.ComponentProps<typeof DayPicker>;

function Calendar({
  className,
  classNames,
  showOutsideDays = true,
  ...props
}: CalendarProps) {
  const [locale] = useLocale("picker");
  return (
    <DayPicker
      locale={locale.dayPicker}
      showOutsideDays={showOutsideDays}
      className={cnBase("oui-p-3 oui-bg-base-7 oui-rounded", className)}
      classNames={{
        months:
          "oui-flex oui-flex-col sm:oui-flex-row oui-space-y-4 sm:oui-gap-x-4 sm:oui-space-y-0",
        month: "oui-space-y-4",
        caption:
          "oui-flex oui-justify-center oui-pt-1 oui-relative oui-items-center",
        caption_label: "oui-text-sm oui-font-medium ",
        nav: "oui-gap-x-1 oui-flex oui-items-center",
        nav_button: cnBase(
          "oui-h-7 oui-w-7 oui-bg-transparent oui-p-0 oui-opacity-50 hover:oui-opacity-100",
        ),
        nav_button_previous: "oui-absolute oui-start-1",
        nav_button_next: "oui-absolute oui-end-1",
        table: "oui-w-full oui-border-collapse oui-space-y-1",
        head_row: "oui-flex",
        head_cell:
          "oui-text-base-contrast-80 oui-rounded-md oui-w-7 oui-font-normal oui-text-[0.8rem] oui-opacity-30",
        row: "oui-flex oui-w-full oui-mt-2",
        cell: cnBase(
          "oui-relative oui-day-cell oui-p-0 oui-text-center oui-text-2xs focus-within:oui-relative focus-within:oui-z-20 [&:has([aria-selected])]:oui-bg-base-4 [&:has([aria-selected].day-outside)]:oui-bg-base-4/50 [&:has([aria-selected].day-range-end)]:oui-rounded-e-md",
          props.mode === "range"
            ? "[&:has(>.oui-day-range-end)]:oui-rounded-e-md [&:has(>.oui-day-range-start)]:oui-rounded-s-md first:[&:has([aria-selected])]:oui-rounded-s-md last:[&:has([aria-selected])]:oui-rounded-e-md"
            : "[&:has([aria-selected])]:oui-rounded-md",
        ),
        day: cnBase(
          //   buttonVariants({ variant: "ghost" }),
          "oui-h-7 oui-w-7 oui-p-0 oui-rounded-md oui-font-normal aria-selected:oui-opacity-100 oui-text-base-contrast-80 hover:oui-bg-base-5",
        ),
        day_range_start:
          "day-range-start  oui-bg-primary-darken oui-text-base-contrast-80 hover:oui-bg-primary-darken",
        day_range_end:
          "day-range-end oui-bg-primary-darken oui-text-base-contrast-80 hover:oui-bg-primary-darken",
        day_selected: "oui-bg-base-4 ",
        day_today: "oui-bg-accent ",
        day_outside:
          "oui-day-outside oui-text-muted-foreground oui-opacity-50 aria-selected:oui-bg-accent/50 aria-selected:oui-text-muted-foreground aria-selected:oui-opacity-30",
        day_disabled: "oui-text-muted-foreground oui-opacity-50",
        day_range_middle:
          "aria-selected:oui-bg-accent aria-selected:oui-text-accent-foreground",
        day_hidden: "oui-invisible",
        ...classNames,
      }}
      components={{
        IconLeft: ({ ...props }) => (
          <ChevronLeftIcon size={16} className="oui-text-inherit" opacity={1} />
        ),
        IconRight: ({ ...props }) => (
          <ChevronRightIcon
            size={16}
            className="oui-text-inherit"
            opacity={1}
          />
        ),
      }}
      {...props}
    />
  );
}

Calendar.displayName = "Calendar";

export { Calendar };
