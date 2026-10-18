import React from "react";
import { usePerformanceScript } from "./performance.script";
import { PerformanceUI } from "./performance.ui";

export const PerformanceWidget: React.FC = () => {
  const state = usePerformanceScript();
  return <PerformanceUI {...state} />;
};
