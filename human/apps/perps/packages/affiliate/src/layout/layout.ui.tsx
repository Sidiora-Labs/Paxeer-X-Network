import React, { PropsWithChildren } from "react";
import { Box } from "@paxeer-network/ui";
import { Scaffold, SideBar, SideBarProps } from "@paxeer-network/ui-scaffold";

export type AffiliateLayoutProps = {} & SideBarProps;

export const AffiliateLayout: React.FC<
  PropsWithChildren<AffiliateLayoutProps>
> = (props) => {
  const { ...rest } = props;
  return (
    <Scaffold leftSidebar={<LeftSidebar {...rest} />}>
      <Box mx={3} my={6}>
        {props.children}
      </Box>
    </Scaffold>
  );
};

const LeftSidebar: React.FC<SideBarProps> = (props) => {
  return (
    <Box p={4} m={3} border borderColor={8} r={"xl"}>
      <SideBar {...props} />
    </Box>
  );
};
