import { positionToPath } from "../pathMap";
import { ExtensionPositionEnum } from "../types";

describe("positionToPath", () => {
  it("returns mapped path for ExtensionPositionEnum values", () => {
    expect(positionToPath(ExtensionPositionEnum.DepositForm)).toBe(
      "Deposit.DepositForm",
    );
    expect(positionToPath(ExtensionPositionEnum.WithdrawForm)).toBe(
      "Deposit.WithdrawForm",
    );
    expect(positionToPath(ExtensionPositionEnum.AccountMenu)).toBe(
      "Account.AccountMenu",
    );
    expect(positionToPath(ExtensionPositionEnum.MobileAccountMenu)).toBe(
      "Account.MobileAccountMenu",
    );
    expect(positionToPath(ExtensionPositionEnum.MainMenus)).toBe(
      "Layout.MainMenus",
    );
    expect(positionToPath(ExtensionPositionEnum.EmptyDataIdentifier)).toBe(
      "Table.EmptyDataIdentifier",
    );
  });

  it("returns string as-is when it contains a dot (path string)", () => {
    expect(positionToPath("Custom.Section.Component")).toBe(
      "Custom.Section.Component",
    );
  });

  it("falls back to String(position) for unknown enum or missing mapping", () => {
    expect(positionToPath("unknownPosition" as any)).toBe("unknownPosition");
  });
});
