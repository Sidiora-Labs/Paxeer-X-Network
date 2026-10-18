/* eslint-disable @typescript-eslint/no-explicit-any */
import { useRef } from "react";

type NotFunction<T> = T extends (...args: any[]) => any ? never : T;

export const useUpdatedRef = <T>(val: NotFunction<T>) => {
  const latestRef = useRef<T>(val);
  latestRef.current = val;
  return latestRef;
};
