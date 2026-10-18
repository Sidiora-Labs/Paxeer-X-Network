import { EventEmitter } from "@paxeer-network/core";

class DataCenter extends EventEmitter {
  constructor() {
    super();
  }

  observe = (key: string, callback: (value: any) => void) => {
    // merge data
    this.on(key, callback);
  };

  unobserve = (key: string, callback: (value: any) => void) => {
    this.off(key, callback);
  };
}
