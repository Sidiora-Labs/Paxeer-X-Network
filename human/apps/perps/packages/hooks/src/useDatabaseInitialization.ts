import { useState } from "react";
import { useEffect } from "react";
import {
  DEFAULT_DATABASE_CONFIG,
  initializeAppDatabase,
} from "./middleware/indexedDBManager";

export const useDatabaseInitialization = () => {
  const [isInitialized, setIsInitialized] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  useEffect(() => {
    const initialize = async () => {
      try {
        await initializeAppDatabase(DEFAULT_DATABASE_CONFIG);
        setIsInitialized(true);
      } catch (err) {
        setError(err as Error);
      }
    };

    initialize();
  }, []);

  return { isInitialized, error };
};
