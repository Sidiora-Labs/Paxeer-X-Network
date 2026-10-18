# table — Directory Index

## Directory Responsibility

DataTable component with sort, pagination, pinning, and features (e.g. download). Includes hooks (usePagination, useMultiSort, useSort, useSyncScroll, etc.), EmptyDataState, MultiSortHeader, and table subcomponents (TableHeader, TableBody, TableCell, TablePagination, TablePlaceholder).

## Files

| File                                                             | Language   | Summary                                                                                        | Link                                                             |
| ---------------------------------------------------------------- | ---------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------- |
| index.tsx                                                        | TSX        | Re-exports DataTable, DataFilter, types, hooks, EmptyDataState, MultiSortHeader, TableFeatures | [index.md](index.md)                                             |
| dataTable.tsx                                                    | TSX        | DataTable component                                                                            | [dataTable.md](dataTable.md)                                     |
| dataFilter.tsx                                                   | TSX        | DataFilter component                                                                           | [dataFilter.md](dataFilter.md)                                   |
| emptyDataState.tsx                                               | TSX        | EmptyDataState                                                                                 | [emptyDataState.md](emptyDataState.md)                           |
| multiSortHeader.tsx                                              | TSX        | MultiSortHeader                                                                                | [multiSortHeader.md](multiSortHeader.md)                         |
| tableBody.tsx                                                    | TSX        | TableBody                                                                                      | [tableBody.md](tableBody.md)                                     |
| tableCell.tsx                                                    | TSX        | TableCell                                                                                      | [tableCell.md](tableCell.md)                                     |
| tableHeader.tsx                                                  | TSX        | TableHeader                                                                                    | [tableHeader.md](tableHeader.md)                                 |
| tablePagination.tsx                                              | TSX        | TablePagination                                                                                | [tablePagination.md](tablePagination.md)                         |
| tablePlaceholder.tsx                                             | TSX        | TablePlaceholder                                                                               | [tablePlaceholder.md](tablePlaceholder.md)                       |
| type.ts                                                          | TypeScript | Table types                                                                                    | [type.md](type.md)                                               |
| className.ts                                                     | TypeScript | Table class names                                                                              | [className.md](className.md)                                     |
| transform.ts                                                     | TypeScript | Table transform helpers                                                                        | [transform.md](transform.md)                                     |
| icons.tsx                                                        | TSX        | Table icons                                                                                    | [icons.md](icons.md)                                             |
| [hooks/useInit.ts](hooks/useInit.md)                             | TypeScript | useInit                                                                                        | [hooks/useInit.md](hooks/useInit.md)                             |
| [hooks/usePagination.ts](hooks/usePagination.md)                 | TypeScript | usePagination                                                                                  | [hooks/usePagination.md](hooks/usePagination.md)                 |
| [hooks/useMultiSort.ts](hooks/useMultiSort.md)                   | TypeScript | useMultiSort                                                                                   | [hooks/useMultiSort.md](hooks/useMultiSort.md)                   |
| [hooks/useSort.ts](hooks/useSort.md)                             | TypeScript | useSort                                                                                        | [hooks/useSort.md](hooks/useSort.md)                             |
| [hooks/useSyncScroll.ts](hooks/useSyncScroll.md)                 | TypeScript | useSyncScroll                                                                                  | [hooks/useSyncScroll.md](hooks/useSyncScroll.md)                 |
| [hooks/useShowHeader.ts](hooks/useShowHeader.md)                 | TypeScript | useShowHeader                                                                                  | [hooks/useShowHeader.md](hooks/useShowHeader.md)                 |
| [hooks/useShowPagination.ts](hooks/useShowPagination.md)         | TypeScript | useShowPagination                                                                              | [hooks/useShowPagination.md](hooks/useShowPagination.md)         |
| [hooks/useScroll.ts](hooks/useScroll.md)                         | TypeScript | useScroll (table)                                                                              | [hooks/useScroll.md](hooks/useScroll.md)                         |
| [hooks/useWrap.ts](hooks/useWrap.md)                             | TypeScript | useWrap                                                                                        | [hooks/useWrap.md](hooks/useWrap.md)                             |
| [features/index.ts](features/index.md)                           | TypeScript | TableFeatures barrel                                                                           | [features/index.md](features/index.md)                           |
| [features/download.ts](features/download.md)                     | TypeScript | Download feature                                                                               | [features/download.md](features/download.md)                     |
| [utils/getColumnPinningProps.ts](utils/getColumnPinningProps.md) | TypeScript | Column pinning props                                                                           | [utils/getColumnPinningProps.md](utils/getColumnPinningProps.md) |

## Key Entities

| Entity                                 | File               | Role                                     |
| -------------------------------------- | ------------------ | ---------------------------------------- |
| DataTable                              | dataTable.tsx      | Main table component with DataTableProps |
| DataFilter                             | dataFilter.tsx     | Filter UI for table                      |
| usePagination / useMultiSort / useSort | hooks/\*.ts        | Table behavior hooks                     |
| TableFeatures                          | features/          | e.g. download                            |
| EmptyDataState                         | emptyDataState.tsx | Empty state for table                    |
