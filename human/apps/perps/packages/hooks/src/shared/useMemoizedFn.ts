/* eslint-disable @typescript-eslint/no-explicit-any */
import { useMemo, useRef } from "react";

type noop = (this: any, ...args: any[]) => any;

type PickFunction<T extends noop> = (
  this: ThisParameterType<T>,
  ...args: Parameters<T>
) => ReturnType<T>;

export const useMemoizedFn = <T extends noop>(fn?: T) => {
  if (typeof fn !== "function") {
    console.error(
      `useMemoizedFn expected parameter a function, got ${typeof fn}`,
    );
  }

  const safeFn = typeof fn === "function" ? fn : ((() => {}) as T);

  const fnRef = useRef<T>(safeFn);

  fnRef.current = useMemo<T>(() => safeFn, [safeFn]);

  const wrapperRef = useRef<PickFunction<T> | null>(null);

  if (!wrapperRef.current) {
    wrapperRef.current = function (this, ...args) {
      return fnRef.current.apply(this, args);
    };
  }

  return wrapperRef.current;
};
