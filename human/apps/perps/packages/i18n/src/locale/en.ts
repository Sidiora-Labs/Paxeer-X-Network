import { affiliate } from "./module/affiliate";
import { chart } from "./module/chart";
import { common } from "./module/common";
import { connector } from "./module/connector";
import { leverage } from "./module/leverage";
import { markets } from "./module/markets";
import { navigation } from "./module/navigation";
import { notification } from "./module/notification";
import { orderEntry } from "./module/orderEntry";
import { orders } from "./module/orders";
import { portfolio } from "./module/portfolio";
import { positions } from "./module/positions";
import { scaffold } from "./module/scaffold";
import { share } from "./module/share";
import { tpsl } from "./module/tpsl";
import { trading } from "./module/trading";
import { tradingLeaderboard } from "./module/tradingLeaderboard";
import { tradingRewards } from "./module/tradingRewards";
import { tradingView } from "./module/tradingView";
import { transfer } from "./module/transfer";
import { ui } from "./module/ui";
import { vaults } from "./module/vaults";
import { widget } from "./module/widget";

export const en = {
  ...common,
  ...markets,
  ...portfolio,
  ...trading,
  ...chart,
  ...positions,
  ...orders,
  ...tpsl,
  ...share,
  ...orderEntry,
  ...leverage,
  ...scaffold,
  ...tradingRewards,
  ...tradingView,
  ...connector,
  ...transfer,
  ...affiliate,
  ...ui,
  ...tradingLeaderboard,
  ...widget,
  ...vaults,
  ...notification,
  ...navigation,
};
