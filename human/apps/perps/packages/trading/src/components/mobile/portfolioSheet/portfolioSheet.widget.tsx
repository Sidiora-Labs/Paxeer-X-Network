import { usePortfolioSheetScript } from "./portfolioSheet.script";
import { PortfolioSheet } from "./portfolioSheet.ui";

export const PortfolioSheetWidget = () => {
  const state = usePortfolioSheetScript();
  return <PortfolioSheet {...state} />;
};
