import { FC } from "react";
import { Flex } from "../flex";
import { PaginationProps, Paginations } from "../pagination";

export const TablePagination: FC<Omit<PaginationProps, "classNames">> = (
  props,
) => {
  if (props.pageTotal === 0) {
    return null;
  }
  return (
    <Flex justify={"end"} py={2} mx={3} className="oui-h-10 oui-w-full">
      <Paginations
        {...props}
        className="oui-table-pagination oui-justify-between oui-w-full"
      />
    </Flex>
  );
};
