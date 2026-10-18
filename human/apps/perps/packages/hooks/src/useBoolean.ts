import { useCallback, useState } from "react";

export const useBoolean = (
  initialValue = false
): [
  boolean,
  {
    setTrue: () => void;
    setFalse: () => void;
    toggle: () => void;
  }
] => {
  const [value, setValue] = useState(initialValue);
  const setTrue = useCallback(() => setValue(true), []);
  const setFalse = useCallback(() => setValue(false), []);
  const toggle = useCallback(() => setValue((v) => !v), []);
  return [value, { setTrue, setFalse, toggle }];
};
