import useConstant from "use-constant";
import { EventEmitter, SimpleDI } from "@paxeer-network/core";

export const useEventEmitter = () => {
  return useConstant(() => {
    let ee = SimpleDI.get<EventEmitter>("EE");

    if (!ee) {
      ee = new EventEmitter();

      SimpleDI.registerByName("EE", ee);
    }
    return ee;
  });
};
