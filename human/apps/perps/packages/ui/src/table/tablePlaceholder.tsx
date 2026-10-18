import { FC, ReactNode } from "react";
import { cnBase } from "tailwind-variants";
import { Box } from "../box";
import { Spinner } from "../spinner/spinner";
import { InjectableEmptyDataState } from "./emptyDataState";

type TablePlaceholderProps = {
  visible?: boolean;
  loading?: boolean;
  emptyView?: ReactNode;
  className?: string;
};

export const TablePlaceholder: FC<TablePlaceholderProps> = (props) => {
  const { visible, loading, emptyView, className } = props;
  if (!visible) {
    return null;
  }
  return (
    <Box
      position="absolute"
      left={0}
      top={0}
      right={0}
      bottom={0}
      className={cnBase(
        "oui-flex oui-items-center oui-justify-center",
        className,
      )}
    >
      {loading ? <Spinner /> : emptyView || <InjectableEmptyDataState />}
    </Box>
  );
};
