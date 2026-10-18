export const windowGuard = (cb: Function) => {
  if (typeof window !== "undefined") {
    cb();
  }
};

export const getGlobalObject = () => {
  if (typeof globalThis !== "undefined") {
    return globalThis;
  }
  if (typeof self !== "undefined") {
    return self;
  }
  if (typeof window !== "undefined") {
    return window;
  }
  // @ts-ignore
  if (typeof global !== "undefined") {
    // @ts-ignore
    return global;
  }
  throw new Error("cannot find the global object");
};


/// get timestamp
export const getTimestamp = (): number => {

  if (typeof window !== "undefined") {
    // @ts-ignore
    const timeOffset = getGlobalObject()?.__PAXEER_NETWORK_timestamp_offset;
    if (typeof timeOffset === 'number') {
      return Date.now() + (timeOffset || 0);
    }
  }
  return Date.now();
}