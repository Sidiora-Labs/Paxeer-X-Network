import { useCallback, useState } from "react";

export const useLeftNavScript = () => {
  const [open, setOpen] = useState(false);

  const showSheet = useCallback(() => {
    setOpen(true);
  }, []);

  const hideSheet = useCallback(() => {
    setOpen(false);
  }, []);

  return {
    open,
    onOpenChange: setOpen,
    showSheet,
    hideSheet,
  };
};

export type LeftNavScriptReturn = ReturnType<typeof useLeftNavScript>;
