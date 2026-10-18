import { APIManager } from "./apiManager.ui";
import { useApiManagerScript } from "./apiManager.script";


export const APIManagerWidget = (props?: {
  filterTags?: [string];
  keyStatus?: string;
}) => {
  const state = useApiManagerScript(props);
  return <APIManager {...state} />;
};
