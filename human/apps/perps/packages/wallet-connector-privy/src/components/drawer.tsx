import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useMediaQuery } from "@paxeer-network/hooks";
import { cn, useScreen } from "@paxeer-network/ui";

interface DrawerProps {
  children: React.ReactNode;
  isOpen: boolean;
  onClose: () => void;
}

export function Drawer({ children, isOpen, onClose }: DrawerProps) {
  const [windowHeight, setWindowHeight] = useState(0);
  const { isMobile } = useScreen();

  useEffect(() => {
    setWindowHeight(window.innerHeight);

    const handleResize = () => {
      setWindowHeight(window.innerHeight);
    };

    window.addEventListener("resize", handleResize);

    if (isOpen) {
      document.body.style.overflow = "hidden";
    } else {
      document.body.style.overflow = "auto";
    }

    return () => {
      document.body.style.overflow = "auto";
      window.removeEventListener("resize", handleResize);
    };
  }, [isOpen, isMobile]);

  if (!isOpen) return null;

  const drawerHeight = isMobile ? windowHeight : windowHeight - 72 - 24;

  return createPortal(
    <div className="oui-fixed oui-inset-0 oui-z-[60]">
      <div
        className="oui-absolute oui-inset-0 oui-bg-[rgba(0,0,0,0.48)] oui-transition-opacity"
        onClick={onClose}
      />

      <div
        style={{ height: `${drawerHeight}px` }}
        className={cn(
          "oui-fixed oui-top-0 oui-end-0",
          "oui-bg-base-9 oui-shadow-lg",
          "oui-border oui-border-line-12",
          "oui-w-[276px]",
          "md:oui-w-[300px]",
          "md:oui-top-1/2 md:oui-translate-y-[-50%]",
          "oui-rounded-0 md:oui-rounded-2xl",
          "md:oui-rounded-0",
          "oui-p-4",
          "oui-transform oui-transition-transform oui-duration-300 oui-ease-in-out",
          "oui-flex oui-flex-col oui-justify-between",
          "oui-m-0 md:oui-m-3",
          isOpen ? "oui-translate-x-0" : "oui--translate-x-full",
        )}
      >
        {children}
      </div>
    </div>,
    document.body,
  );
}
