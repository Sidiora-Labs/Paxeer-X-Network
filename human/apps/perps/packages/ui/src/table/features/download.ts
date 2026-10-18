import { Table, TableFeature, RowData } from "@tanstack/react-table";
import { Column } from "../type";

// Define types for our new feature's table APIs
export interface DownloadInstance {
  getPlainTextData: () => any[];
  download: (filename?: string) => void;
}

// Use declaration merging to add our new feature APIs and state types to TanStack Table's existing types.
declare module "@tanstack/react-table" {
  //merge our new feature's instance APIs with the existing table instance APIs
  interface Table<TData extends RowData> extends DownloadInstance {}
}

export const DownloadFeature: TableFeature<any> = {
  // define the new feature's table instance methods
  createTable: <TData extends RowData>(table: Table<TData>): void => {
    table.getPlainTextData = () => {
      const header = table
        .getAllColumns()
        // filter action column
        .filter((column) => (column.columnDef.meta as any).type !== "action")
        .map((column) => {
          const { title, plantTextTitle } = column.columnDef.meta as any;
          return plantTextTitle || title;
        });

      const rows = table.getRowModel().rows.map((row) =>
        row
          .getVisibleCells()
          .filter(
            (cell) => (cell.column.columnDef.meta as any).type !== "action",
          )
          .map((cell) => {
            const { original: record, index } = cell.row;
            const { renderPlantText } = (cell.column.columnDef.meta ||
              {}) as Column;

            let value = cell.getValue();

            if (typeof renderPlantText === "function") {
              value = renderPlantText(value, record, index, cell);
            }

            return `"${value}"`;
          }),
      );

      const data = [header, ...rows];

      return data;
    };

    table.download = (filename?: string) => {
      const data = table.getPlainTextData();
      console.log("downloadCSV", data);
      downloadCSV(data, filename);
    };
  },
};

function generateCSV(data: any[]) {
  return data.map((row) => row.join(",")).join("\n");
}

function downloadCSV(data: any[], filename = `${Date.now()}.csv`) {
  const csvContent = generateCSV(data);
  const blob = new Blob([csvContent], { type: "text/csv;charset=utf-8;" });
  const url = URL.createObjectURL(blob);

  const link = document.createElement("a");
  link.href = url;
  link.setAttribute("download", filename);
  link.click();

  URL.revokeObjectURL(url);
}
