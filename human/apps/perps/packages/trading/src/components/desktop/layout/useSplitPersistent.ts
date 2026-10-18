import { useMemo } from "react";

export const useSplitPersistent = (
  key: string,
  defaulValue?: string,
  dep?: any,
): [string | undefined, (size: string) => void] => {
  const size = useMemo(() => {
    const size = localStorage.getItem(key);

    if (size) {
      return `${size}%`;
    }
    return defaulValue;
  }, [key, defaulValue, dep]);

  const setSize = (size: string) => {
    localStorage.setItem(key, size);
  };

  return [size, setSize];
};
