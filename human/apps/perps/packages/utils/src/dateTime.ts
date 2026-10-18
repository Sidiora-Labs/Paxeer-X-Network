export const timeConvertString = (time: number): number[] => {
  time /= 1000;
  const h = Math.floor(time / 3600);
  const m = Math.floor((time / 60) % 60);
  const s = Math.floor(time % 60);
  return [h, m, s];
};

/// will be return 'yyyy-mm-dd hh:MM:ss'
export const timestampToString = (timestamp: number): string => {
  const date = new Date(timestamp);
  const year = date.getFullYear();
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  const hours = String(date.getHours()).padStart(2, "0");
  const minutes = String(date.getMinutes()).padStart(2, "0");
  const seconds = String(date.getSeconds()).padStart(2, "0");

  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
};

export function subtractDaysFromCurrentDate(
  days: number,
  startDate?: Date
): Date {
  const currentDate = startDate || new Date();
  const resultDate = new Date(currentDate);
  resultDate.setDate(currentDate.getDate() - days);
  return resultDate;
}
