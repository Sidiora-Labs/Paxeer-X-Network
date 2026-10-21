/* LayerX UI — public API */

// lib
export { cn } from "./lib/utils";
export {
  formatMoney,
  formatBalance,
  formatRecency,
  monthBandLabel,
  downloadCsv,
  type FormatMoneyOptions,
} from "./lib/format";
export {
  PlatformProvider,
  PlatformSwitch,
  usePlatform,
  useMediaQuery,
  type Platform,
  type PlatformSetting,
} from "./lib/platform";
export {
  recencyOf,
  type MoneyItem,
  type MoneyGroup,
  type NotificationItem,
  type RecencySegment,
} from "./lib/types";

// primitives
export { Button, IconButton, buttonVariants, iconButtonVariants, type ButtonProps } from "./components/button";
export { Input, SearchInput, type InputProps, type SearchInputProps } from "./components/input";
export { Badge, badgeVariants, type BadgeProps } from "./components/badge";
export { Avatar, avatarVariants, type AvatarProps } from "./components/avatar";
export { Card, cardVariants, type CardProps } from "./components/card";
export { Switch } from "./components/switch";
export { SegmentedControl, type SegmentedControlProps, type SegmentedControlOption } from "./components/segmented-control";
export { List, ListItem, IconTile, SectionHeader, ViewAllChip, Divider, type ListItemProps } from "./components/list";
export { AmountText, type AmountTextProps } from "./components/amount";
export { Stat, StatPair } from "./components/stat";
export { EmptyState } from "./components/empty-state";
export { Spinner, Skeleton, SkeletonRow } from "./components/feedback";
export { OptionList, type OptionListItem } from "./components/option-list";
export { BalanceHeader, type BalanceHeaderProps } from "./components/balance-header";
export { QuickActions, type QuickAction } from "./components/quick-actions";
export { BankCard, CardCarousel, type BankCardData } from "./components/bank-card";

// overlays
export { Sheet, SheetHeader, SheetBody, SheetFooter, SheetDescription, type SheetProps } from "./components/sheet";
export { Modal, ModalHeader, ModalBody, ModalFooter, type ModalProps } from "./components/modal";
export { Drawer, DrawerHeader, DrawerBody, DrawerFooter, type DrawerProps } from "./components/drawer";
export { ResponsiveDialog, ConfirmDialog, type ConfirmAction } from "./components/responsive-dialog";
export { Popover, PopoverTrigger, PopoverContent } from "./components/popover";
export { CalendarRangePicker, type DateRange } from "./components/calendar-range-picker";
export { CodeInput, Keypad, type CodeInputProps } from "./components/code-input";

// patterns (responsive per the LayerX mobile/desktop contract)
export { AppShell, BottomTabBar, Sidebar, type AppShellProps, type NavItem } from "./components/app-shell";
export { PrimaryAction } from "./components/primary-action";
export { DetailDisclosure } from "./components/detail";
export { FilterBar, isFilterActive, type FilterDef, type FilterValues } from "./components/filters";
export { MoneyList } from "./components/money-list";
export { Wizard, type WizardStep, type WizardSummaryItem } from "./components/wizard";
export { GlobalSearch, type SearchResultGroup, type SearchResultItem } from "./components/search";
export { CodeEntry } from "./components/code-entry";
export { NotificationsScreen, BellPopover, NotificationsArchive } from "./components/notifications";
