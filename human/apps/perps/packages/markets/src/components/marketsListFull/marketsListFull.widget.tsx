import { SortType } from "../../type";
import {
  MarketsListFullType,
  useMarketsListFullScript,
} from "./marketsListFull.script";
import { MarketsListFull } from "./marketsListFull.ui";

export type MarketsListFullWidgetProps = {
  type?: MarketsListFullType;
  initialSort?: SortType;
  dataFilter?: (data: any[]) => any[];
};

export const MarketsListFullWidget: React.FC<MarketsListFullWidgetProps> = (
  props,
) => {
  const state = useMarketsListFullScript(props);

  return <MarketsListFull {...state} type={props.type} />;
};
