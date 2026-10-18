import { injectable } from "@paxeer-network/ui";
import { SymbolInfoBarFull } from "./symbolInfoBarFull.ui";

export const SymbolInfoBarFullInjectable = injectable(
  SymbolInfoBarFull,
  "Trading.SymbolInfoBar.Desktop",
);
