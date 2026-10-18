import { enUS } from "date-fns/locale";

export const localeValues = {
  locale: "en" as string,
  dialog: {
    ok: "OK",
    cancel: "Cancel",
  },
  modal: {
    confirm: "Confirm",
    cancel: "Cancel",
  },
  pagination: {
    morePages: "More pages",
    rowsPerPage: "Rows per page",
  },
  picker: {
    selectDate: "Select Date",
    dayPicker: enUS,
  },
  empty: {
    description: "No results found.",
  },
} as const;
