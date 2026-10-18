import { useContext } from "react";
import { PaxeerNetworkContext } from "./paxeerNetworkContext";

export const useKeyStore = () => {
  const ctx = useContext(PaxeerNetworkContext);

  return ctx.keyStore;
};
