import { BaseIcon } from "./baseIcon";
import { CombineIcon } from "./combine";

export type { BaseIconProps } from "./baseIcon";
export { CloseIcon } from "./close";
export { CheckIcon } from "./check";
export { ChevronDownIcon } from "./chevronDown";
export { ChevronUpIcon } from "./chevronUp";
export { CaretUpIcon } from "./caretUp";
export { CaretDownIcon } from "./caretDown";
export { CaretLeftIcon } from "./caretLeft";
export { CaretRightIcon } from "./caretRight";
export { ChevronLeftIcon } from "./chevronLeft";
export { ChevronRightIcon } from "./chevronRight";
export { CalendarMinusIcon } from "./calendarMinus";
export { SettingIcon } from "./setting";
export { CloseSquareFillIcon } from "./closeSquareFill";
export { CloseCircleFillIcon } from "./closeCircleFill";
export { CheckedCircleFillIcon } from "./checkCircleFill";
export { CheckedSquareFillIcon } from "./checkSquareFill";
export { CheckSquareEmptyIcon } from "./checkSquareEmpty";
export { PlusIcon } from "./plus";
export { ReduceIcon } from "./reduce";
export { CircleOutlinedIcon } from "./circleOutlined";
export { SquareOutlinedIcon } from "./squareOutlined";
export { ExclamationFillIcon } from "./exclamationFill";
export { QuestionFillIcon } from "./questionFill";
export { ArrowLeftRightIcon } from "./arrowLeftRight";
export { ArrowDownUpIcon } from "./arrowDownUp";
export { ArrowUpSquareFillIcon } from "./arrowUpSquareFill";
export { ArrowDownSquareFillIcon } from "./arrowDownSquareFill";
export { ArrowLeftRightSquareFill } from "./arrowLeftRightSquareFill";
export { ArrowRightUpSquareFillIcon } from "./arrowRightUpSquareFill";
export { AssetIcon } from "./assetIcon";
export { BarChartIcon } from "./barChartIcon";
export { BattleIcon } from "./battleIcon";
export { BattleActiveIcon } from "./battleActiveIcon";
export { BattleInactiveIcon } from "./battleInactiveIcon";
export { BattleSolidActiveIcon } from "./battleSolidActiveIcon";
export { BattleSolidInactiveIcon } from "./battleSolidInactiveIcon";
export { FeeTierIcon } from "./feeTier";
export { EarnActiveIcon } from "./earnActiveIcon";
export { EarnInactiveIcon } from "./earnInactiveIcon";
export { EditIcon } from "./edit";
export { EyeIcon } from "./eye";
export { ShareIcon } from "./share";
export { EyeCloseIcon } from "./eyeClose";
export { RefreshIcon } from "./refresh";
export { PaxeerNetworkIcon } from "./paxeer-network";
export { EsPaxeerNetworkIcon } from "./esPaxeerNetwork";
export { InfoCircleIcon } from "./infoCircle";
export { InfoFillIcon } from "./infoFill";

export { TokenIcon } from "./tokenIcon";
export { ChainIcon } from "./chainIcon";
export { WalletIcon } from "./walletIcon";
export { CalendarIcon } from "./calendar";
export { CopyIcon } from "./copy";
export { ServerFillIcon } from "./serverFill";
export { SortingAscIcon } from "./sortingASCIcon";
export { SortingDescIcon } from "./sortingDESCIcon";
export {
  ArrowUpShortIcon,
  ArrowDownShortIcon,
  ArrowLeftShortIcon,
  ArrowRightShortIcon,
} from "./arrowShort";

export { SortingIcon } from "./sortingIcon";
export { TraderMobileIcon } from "./traderMobileIcon";
export { AffiliateIcon } from "./affiliateIcon";
export { TradingRewardsIcon } from "./tradingRewardsIcon";
export { PortfolioActiveIcon } from "./portfolioActiveIcon";
export { PortfolioInactiveIcon } from "./portfolioInactiveIcon";
export { TradingActiveIcon } from "./tradingActiveIcon";
export { TradingIcon } from "./tradingIcon";
export { TradingInactiveIcon } from "./tradingInactiveIcon";
export { LeaderboardActiveIcon } from "./leaderboardActive";
export { LeaderboardInactiveIcon } from "./leaderboardInactive";
export { MarketsActiveIcon } from "./marketsActiveIcon";
export { MarketsInactiveIcon } from "./marketsInactiveIcon";
export { StarChildChatActiveIcon } from "./starChildChatActiveIcon";
export { StarChildChatInactiveIcon } from "./starChildChatInactiveIcon";
export { CloseRoundFillIcon } from "./closeRoundFill";
export { SelectedChoicesFillIcon } from "./selectedChoicesFill";
export { PopupUnionIcon } from "./popupUnion";
export { SpotIcon } from "./spotIcon";
export { PerpsIcon } from "./perpsIcon";
export { EmptyStateIcon } from "./emptyData";
export { VectorIcon } from "./vectorIcon";
export { SwapHorizIcon } from "./swapHoriz";
export { PeopleIcon } from "./peopleIcon";
export { PersonIcon } from "./personIcon";
export { SettingFillIcon } from "./settingFill";
export { TradingLeftNavIcon } from "./tradingLeftNavIcon";
export { VaultsIcon } from "./vaultsIcon";
export { LeftNavVaultsIcon } from "./leftNavVaultsIcon";
export { NewsFillIcon } from "./newsFill";
export { BellIcon } from "./bellIcon";
export { WoofiStakeIcon } from "./woofiStakeIcon";
export { EarnIcon } from "./earnIcon";
export { ReferralSolidIcon } from "./referralSolidIcon";
export { AddCircleIcon } from "./addCircle";
export { WarningIcon } from "./warning";
export { RwaIcon } from "./rwa";

type IconType = typeof BaseIcon & {
  combine: typeof CombineIcon;
};

const Icon = BaseIcon as IconType;
Icon.combine = CombineIcon;

export { Icon };

export type { IconType };
