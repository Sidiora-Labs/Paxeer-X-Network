"use client";

import { usePathname } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import { ErrorBoundary } from "./error";
import { OfflineBanner } from "./offline";
import { browserSupportReportOutbox } from "./report";

export function ApplicationStateBoundary({ children }: Readonly<{ children: ReactNode }>) {
  const pathname = usePathname();
  useEffect(() => {
    const flush = () => { void browserSupportReportOutbox.flushPending().catch(() => undefined); };
    flush();
    window.addEventListener("online", flush);
    return () => { window.removeEventListener("online", flush); };
  }, []);
  return (
    <>
      <OfflineBanner />
      <ErrorBoundary key={pathname} route={pathname}>{children}</ErrorBoundary>
    </>
  );
}
