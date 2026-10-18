import React from "react";
import { useBottomNavBarScript } from "./bottomNavBar.script";
import { BottomNavBar } from "./bottomNavBar.ui";

export const BottomNavBarWidget: React.FC = () => {
  const state = useBottomNavBarScript();
  return <BottomNavBar {...state} />;
};
