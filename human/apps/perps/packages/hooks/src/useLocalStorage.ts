// https://usehooks-ts.com/react-hook/use-local-storage
import { useCallback, useEffect, useState } from "react";
import { parseJSON } from "./utils/json";

export function useLocalStorage<T>(
  key: string,
  initialValue: T,
  options: {
    parseJSON: typeof parseJSON;
  } = {
    parseJSON: parseJSON,
  },
): [any, (value: T) => void] {
  // Get from local storage then
  // parse stored json or return initialValue
  const readValue = useCallback((): T => {
    // Prevent build error "window is undefined" but keeps working
    if (typeof window === "undefined") {
      return initialValue;
    }

    try {
      const item = window.localStorage.getItem(key);
      return item ? (options.parseJSON(item) as T) : initialValue;
    } catch (error) {
      console.warn(`Error reading localStorage key “${key}”:`, error);
      return initialValue;
    }
  }, [initialValue, key]);

  // State to store our value
  // Pass initial state function to useState so logic is only executed once
  const [storedValue, setStoredValue] = useState<T>(readValue);

  // Return a wrapped version of useState's setter function that ...
  // ... persists the new value to localStorage.
  const setValue: React.Dispatch<T> = useCallback(
    (value: T) => {
      // Prevent build error "window is undefined" but keeps working
      if (typeof window === "undefined") {
        console.warn(
          `Tried setting localStorage key “${key}” even though environment is not a client`,
        );
      }

      try {
        // Allow value to be a function so we have the same API as useState
        const newValue = value instanceof Function ? value(storedValue) : value;

        // Save to local storage
        window.localStorage.setItem(key, JSON.stringify(newValue));
        // dispath event
        window.dispatchEvent(new Event("storage"));

        // Save state
        setStoredValue(() => newValue);

        // We dispatch a custom event so every useLocalStorage hook are notified
        // window.dispatchEvent(new Event('local-storage'))
      } catch (error) {
        console.warn(`Error setting localStorage key “${key}”:`, error);
      }
    },
    [storedValue],
  );

  useEffect(() => {
    setStoredValue(readValue());
  }, []);

  // const handleStorageChange = useCallback(
  //   (event: StorageEvent | CustomEvent) => {
  //     if ((event as StorageEvent)?.key && (event as StorageEvent).key !== key) {
  //       return
  //     }
  //     setStoredValue(readValue())
  //   },
  //   [key, readValue],
  // )

  useEffect(() => {
    const handleStorageChange = (event: StorageEvent | CustomEvent) => {
      if ((event as StorageEvent)?.key && (event as StorageEvent).key !== key) {
        return;
      }
      setStoredValue(readValue());
    };

    window?.addEventListener?.("storage", handleStorageChange);

    return () => {
      window?.removeEventListener?.("storage", handleStorageChange);
    };
  }, [key]);

  return [storedValue, setValue];
}
