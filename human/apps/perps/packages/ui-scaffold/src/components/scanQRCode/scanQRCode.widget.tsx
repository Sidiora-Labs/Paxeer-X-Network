import React from "react";
import { useScanQRCodeScript } from "./scanQRCode.script";
import { ScanQRCode } from "./scanQRCode.ui";

export const ScanQRCodeWidget: React.FC = () => {
  const state = useScanQRCodeScript();
  return <ScanQRCode {...state} />;
};
