import { useEffect, useRef, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { useAppContext } from "@paxeer-network/react-app";

export type UseScanQRCodeScriptReturn = ReturnType<typeof useScanQRCodeScript>;

export const useScanQRCodeScript = () => {
  const [open, setOpen] = useState(false);
  const { widgetConfigs } = useAppContext();
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const [showScanTooltip, setShowScanTooltip] = useLocalStorage(
    "paxeerNetwork_qr_code_scan_tooltip_open",
    true,
  );

  const showDialog = () => {
    setOpen(true);
  };

  const hideDialog = () => {
    setOpen(false);
  };

  const onScanSuccess = (url: string) => {
    if (!isValidURL(url)) {
      return;
    }
    const { onSuccess } = widgetConfigs?.scanQRCode || {};
    if (typeof onSuccess === "function") {
      onSuccess(url);
    } else {
      window.location.href = url;
    }
  };

  useEffect(() => {
    if (showScanTooltip) {
      timerRef.current = setTimeout(() => {
        setShowScanTooltip(false);
      }, 8000);
    }
    return () => {
      if (timerRef.current) {
        clearTimeout(timerRef.current);
      }
    };
  }, [showScanTooltip]);

  return {
    open,
    onOpenChange: setOpen,
    showDialog,
    hideDialog,
    onScanSuccess,
    showScanTooltip,
  };
};

function isValidURL(str: string) {
  try {
    const url = new URL(str);
    return ["http:", "https:"].includes(url.protocol);
  } catch {
    return false;
  }
}
