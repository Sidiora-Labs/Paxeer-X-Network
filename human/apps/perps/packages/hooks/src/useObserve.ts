import { useState } from "react";

export enum Subjects {
  Orders = "orders",
  Positions = "positions",
}

export const useObserve = <T>(
  key: string,
  initialValue: T
  // @ts-ignore
): [T, (value: T) => void] => {};
