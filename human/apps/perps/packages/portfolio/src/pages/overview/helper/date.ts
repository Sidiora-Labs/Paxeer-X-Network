import { differenceInDays, setHours } from "date-fns";

export const parseDateRangeForFilter = (dateRange: {
  from: Date;
  to?: Date;
}) => {
  const { from, to = new Date() } = dateRange;

  const diff = differenceInDays(from, to);

  if (diff === 0) {
    return [from, setHours(to, 23)];
  }

  return [from, to];
};
