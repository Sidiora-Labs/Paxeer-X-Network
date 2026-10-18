import { FC, PropsWithChildren } from "react";
import { useScreen } from "@paxeer-network/ui";
import { usePortfolioLayoutScript } from "./layout.script";
import { PortfolioLayout, PortfolioLayoutProps } from "./layout.ui";
import { PortfolioLayoutMobile } from "./layout.ui.mobile";

export type PortfolioLayoutWidgetProps = PortfolioLayoutProps;

export const PortfolioLayoutWidget: FC<
  PropsWithChildren<PortfolioLayoutWidgetProps>
> = (props) => {
  const state = usePortfolioLayoutScript({
    current: props.leftSideProps?.current,
  });
  const { isMobile } = useScreen();

  return (
    <>
      {isMobile ? (
        // @ts-ignore
        <PortfolioLayoutMobile {...state} {...props} />
      ) : (
        <PortfolioLayout {...state} {...props} />
      )}
    </>
  );
};
