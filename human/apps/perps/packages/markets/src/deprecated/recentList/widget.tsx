import { DataTableClassNames } from "@paxeer-network/ui";
import { GetColumns } from "../../type";
import { useRecentListScript } from "./recentList.script";
import { RecentList } from "./recentList.ui";

export type RecentListWidgetProps = {
  getColumns?: GetColumns;
  collapsed?: boolean;
  tableClassNames?: DataTableClassNames;
  rowClassName?: string;
};

export const RecentListWidget: React.FC<RecentListWidgetProps> = (props) => {
  const state = useRecentListScript();
  return <RecentList {...state} {...props} />;
};
