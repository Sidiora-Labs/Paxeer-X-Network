# icon — Directory Index

## Directory Responsibility

Icon components for the paxeer-network UI: base components (BaseIcon, BaseIconWithPath, CombineIcon) and many named icons (CheckIcon, ChevronDownIcon, WalletIcon, TradingActiveIcon, etc.) exported from icon/index.ts. Used for consistent iconography across the app.

## Files

| File                 | Language   | Summary                                                          | Link                                       |
| -------------------- | ---------- | ---------------------------------------------------------------- | ------------------------------------------ |
| baseIcon.tsx         | TSX        | BaseIcon and BaseIconProps                                       | [baseIcon.md](baseIcon.md)                 |
| baseIconWithPath.tsx | TSX        | BaseIconWithPath for path-based SVG icons                        | [baseIconWithPath.md](baseIconWithPath.md) |
| combine.tsx          | TSX        | CombineIcon (Icon.combine)                                       | [combine.md](combine.md)                   |
| index.ts             | TypeScript | Re-exports all icons and Icon (BaseIcon + combine)               | [index.md](index.md)                       |
| \*.tsx (many)        | TSX        | Named icon components (addCircle, affiliateIcon, arrowDownUp, …) | See links below                            |

## Key Entities

| Entity           | File                   | Role                                                            |
| ---------------- | ---------------------- | --------------------------------------------------------------- |
| Icon / BaseIcon  | baseIcon.tsx, index.ts | Main icon component; Icon.combine for combined icons            |
| BaseIconWithPath | baseIconWithPath.tsx   | Icon from SVG path                                              |
| Named icons      | Various .tsx           | CheckIcon, ChevronDownIcon, WalletIcon, TradingActiveIcon, etc. |

## Named Icon Files (alphabetical)

| File                          | Export                     | File                         | Export                                    |
| ----------------------------- | -------------------------- | ---------------------------- | ----------------------------------------- |
| addCircle.tsx                 | AddCircleIcon              | arrowDownSquareFill.tsx      | ArrowDownSquareFillIcon                   |
| affiliateIcon.tsx             | AffiliateIcon              | arrowDownUp.tsx              | ArrowDownUpIcon                           |
| arrowLeftRight.tsx            | ArrowLeftRightIcon         | arrowLeftRightSquareFill.tsx | ArrowLeftRightSquareFill                  |
| arrowRightUpSquareFill.tsx    | ArrowRightUpSquareFillIcon | arrowShort.tsx               | ArrowUpShortIcon, ArrowDownShortIcon, ... |
| arrowUpSquareFill.tsx         | ArrowUpSquareFillIcon      | assetIcon.tsx                | AssetIcon                                 |
| barChartIcon.tsx              | BarChartIcon               | baseIcon.tsx                 | BaseIcon                                  |
| battleActiveIcon.tsx          | BattleActiveIcon           | battleIcon.tsx               | BattleIcon                                |
| battleInactiveIcon.tsx        | BattleInactiveIcon         | battleSolidActiveIcon.tsx    | BattleSolidActiveIcon                     |
| battleSolidInactiveIcon.tsx   | BattleSolidInactiveIcon    | bellIcon.tsx                 | BellIcon                                  |
| calendar.tsx                  | CalendarIcon               | calendarMinus.tsx            | CalendarMinusIcon                         |
| caretDown.tsx                 | CaretDownIcon              | caretLeft.tsx                | CaretLeftIcon                             |
| caretRight.tsx                | CaretRightIcon             | caretUp.tsx                  | CaretUpIcon                               |
| chainIcon.tsx                 | ChainIcon                  | check.tsx                    | CheckIcon                                 |
| checkCircleFill.tsx           | CheckedCircleFillIcon      | checkSquareEmpty.tsx         | CheckSquareEmptyIcon                      |
| checkSquareFill.tsx           | CheckedSquareFillIcon      | chevronDown.tsx              | ChevronDownIcon                           |
| chevronLeft.tsx               | ChevronLeftIcon            | chevronRight.tsx             | ChevronRightIcon                          |
| chevronUp.tsx                 | ChevronUpIcon              | circleOutlined.tsx           | CircleOutlinedIcon                        |
| close.tsx                     | CloseIcon                  | closeCircleFill.tsx          | CloseCircleFillIcon                       |
| closeRoundFill.tsx            | CloseRoundFillIcon         | closeSquareFill.tsx          | CloseSquareFillIcon                       |
| copy.tsx                      | CopyIcon                   | earnActiveIcon.tsx           | EarnActiveIcon                            |
| earnIcon.tsx                  | EarnIcon                   | earnInactiveIcon.tsx         | EarnInactiveIcon                          |
| edit.tsx                      | EditIcon                   | emptyData.tsx                | EmptyStateIcon                            |
| esPaxeerNetwork.tsx                 | EsPaxeerNetworkIcon              | exclamationFill.tsx          | ExclamationFillIcon                       |
| eye.tsx                       | EyeIcon                    | eyeClose.tsx                 | EyeCloseIcon                              |
| feeTier.tsx                   | FeeTierIcon                | infoCircle.tsx               | InfoCircleIcon                            |
| leaderboardActive.tsx         | LeaderboardActiveIcon      | leaderboardInactive.tsx      | LeaderboardInactiveIcon                   |
| leftNavVaultsIcon.tsx         | LeftNavVaultsIcon          | marketsActiveIcon.tsx        | MarketsActiveIcon                         |
| marketsInactiveIcon.tsx       | MarketsInactiveIcon        | newsFill.tsx                 | NewsFillIcon                              |
| paxeer-network.tsx                   | PaxeerNetworkIcon                | peopleIcon.tsx               | PeopleIcon                                |
| personIcon.tsx                | PersonIcon                 | perpsIcon.tsx                | PerpsIcon                                 |
| plus.tsx                      | PlusIcon                   | popupUnion.tsx               | PopupUnionIcon                            |
| portfolioActiveIcon.tsx       | PortfolioActiveIcon        | portfolioInactiveIcon.tsx    | PortfolioInactiveIcon                     |
| questionFill.tsx              | QuestionFillIcon           | reduce.tsx                   | ReduceIcon                                |
| referralSolidIcon.tsx         | ReferralSolidIcon          | refresh.tsx                  | RefreshIcon                               |
| rwa.tsx                       | RwaIcon                    | selectedChoicesFill.tsx      | SelectedChoicesFillIcon                   |
| serverFill.tsx                | ServerFillIcon             | setting.tsx                  | SettingIcon                               |
| settingFill.tsx               | SettingFillIcon            | share.tsx                    | ShareIcon                                 |
| sortingASCIcon.tsx            | SortingAscIcon             | sortingDESCIcon.tsx          | SortingDescIcon                           |
| sortingIcon.tsx               | SortingIcon                | spotIcon.tsx                 | SpotIcon                                  |
| squareOutlined.tsx            | SquareOutlinedIcon         | starChildChatActiveIcon.tsx  | StarChildChatActiveIcon                   |
| starChildChatInactiveIcon.tsx | StarChildChatInactiveIcon  | swapHoriz.tsx                | SwapHorizIcon                             |
| toasterIcons.tsx              | ToasterIcons               | tokenIcon.tsx                | TokenIcon                                 |
| traderMobileIcon.tsx          | TraderMobileIcon           | tradingActiveIcon.tsx        | TradingActiveIcon                         |
| tradingIcon.tsx               | TradingIcon                | tradingInactiveIcon.tsx      | TradingInactiveIcon                       |
| tradingLeftNavIcon.tsx        | TradingLeftNavIcon         | tradingRewardsIcon.tsx       | TradingRewardsIcon                        |
| vaultsIcon.tsx                | VaultsIcon                 | vectorIcon.tsx               | VectorIcon                                |
| walletIcon.tsx                | WalletIcon                 | warning.tsx                  | WarningIcon                               |
| woofiStakeIcon.tsx            | WoofiStakeIcon             |                              |                                           |
