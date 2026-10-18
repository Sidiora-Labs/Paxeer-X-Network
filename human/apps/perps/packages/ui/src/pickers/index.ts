export { Calendar } from "./date/calendar";

import { DatePicker as DatePickerBase } from "./datepicker";
import { DateRangePicker } from "./dateRangePicker";

type DatePickerType = typeof DatePickerBase & {
  range: typeof DateRangePicker;
};

const DatePicker = DatePickerBase as DatePickerType;
DatePicker.range = DateRangePicker;

export { DatePicker };
export type { DatePickerProps } from "./datepicker";

export { Picker } from "./picker";
