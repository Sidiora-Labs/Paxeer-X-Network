import { ExtensionPosition, ExtensionPositionEnum } from "./types";

const POSITION_TO_PATH: Record<string, string> = {
  [ExtensionPositionEnum.DepositForm]: "Deposit.DepositForm",
  [ExtensionPositionEnum.WithdrawForm]: "Deposit.WithdrawForm",
  [ExtensionPositionEnum.AccountMenu]: "Account.AccountMenu",
  [ExtensionPositionEnum.MobileAccountMenu]: "Account.MobileAccountMenu",
  [ExtensionPositionEnum.MainMenus]: "Layout.MainMenus",
  [ExtensionPositionEnum.EmptyDataIdentifier]: "Table.EmptyDataIdentifier",
  // EmptyDataState is alias for EmptyDataIdentifier (same enum value)
};

/**
 * Converts ExtensionPosition to interceptor path.
 * @deprecated Use path strings directly (e.g. 'Deposit.DepositForm') instead of ExtensionPosition
 */
export function positionToPath(position: ExtensionPosition): string {
  if (typeof position === "string" && position.includes(".")) {
    return position;
  }
  return POSITION_TO_PATH[position] ?? String(position);
}
