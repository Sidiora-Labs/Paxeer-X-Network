import React from "react";
import { SideBar, SideBarProps } from "./sidebar.ui";
import { useSideNavBuilder } from "./useSideNavBuilder.script";

export const SideNavbarWidget: React.FC<Partial<SideBarProps>> = (props) => {
  const state = useSideNavBuilder(props);
  return <SideBar {...state} />;
};
