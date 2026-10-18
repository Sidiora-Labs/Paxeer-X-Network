import { DataTableClassNames } from "@paxeer-network/ui";
import { GetColumns } from "../../type";
import { useNewListingListScript } from "./newListingList.script";
import { NewListingList } from "./newListingList.ui";

export type NewListingListWidgetProps = {
  getColumns?: GetColumns;
  collapsed?: boolean;
  tableClassNames?: DataTableClassNames;
  rowClassName?: string;
};

export const NewListingListWidget: React.FC<NewListingListWidgetProps> = (
  props,
) => {
  const state = useNewListingListScript();
  return <NewListingList {...state} {...props} />;
};
