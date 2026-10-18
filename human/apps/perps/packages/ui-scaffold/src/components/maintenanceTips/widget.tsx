import { MaintenanceTipsUI } from "./ui";
import { useMaintenanceScript } from "./script";

export const MaintenanceTipsWidget = () => {
  const props = useMaintenanceScript();
  return <MaintenanceTipsUI {...props} />;
};
