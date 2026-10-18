export const generateKeyFun =
  (args: {
    path: string;
    //** YYYY-MM-dd */
    startDate?: string;
    //** YYYY-MM-dd */
    endDate?: string;
    //** default is 100 */
    size?: number;
    page?: number;
    sort?: string;
  }) =>
  (pageIndex: number, previousPageData: any): string | null => {
    // reached the end
    if (previousPageData && !previousPageData.rows?.length) {
      return null;
    }

    const { path, startDate, endDate, size = 100, page, sort } = args;

    const search = new URLSearchParams([
      ["size", size.toString()],
      ["page", `${page || pageIndex + 1}`],
    ]);

    if (sort) {
      search.set("sort", sort);
    }

    if (startDate) {
      search.set(`start_date`, startDate);
    }

    if (endDate) {
      search.set(`end_date`, endDate);
    }

    return `${path}?${search.toString()}`;
  };
