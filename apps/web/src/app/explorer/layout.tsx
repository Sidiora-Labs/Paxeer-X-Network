import type { ReactNode } from "react";

export const dynamic = "force-static";
export const revalidate = 60;

export default function ExplorerPlaneLayout({ children }: Readonly<{ children: ReactNode }>) {
  return <>{children}</>;
}
