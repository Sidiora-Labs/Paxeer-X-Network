import { SimpleDI } from "@paxeer-network/core";

export const useSimpleDI = <T>() => {
  return {
    get: SimpleDI.get<T>,
    // getOr: SimpleDI.getOr<T>(name, SimpleDI.get<T>(name)),
    register: SimpleDI.register,
  };
};
