import type { Metadata, Viewport } from "next";
import { Geist, Geist_Mono } from "next/font/google";

import { Providers } from "@/components/bridge/providers";
import "./globals.css";

const geistSans = Geist({
  variable: "--font-geist-sans",
  subsets: ["latin"],
});

const geistMono = Geist_Mono({
  variable: "--font-geist-mono",
  subsets: ["latin"],
});

export const metadata: Metadata = {
  title: "Paxeer Bridge — move assets to and from Paxeer",
  description:
    "The attested PaxeerX bridge: lock assets on Ethereum, Base, Arbitrum, Optimism, BNB Chain, Polygon, Avalanche, HyperEVM or Solana and receive them on Paxeer — and back.",
  applicationName: "Paxeer Bridge",
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: [
    { media: "(prefers-color-scheme: light)", color: "#ffffff" },
    { media: "(prefers-color-scheme: dark)", color: "#121317" },
  ],
};

/** Applies the stored dx theme before first paint (the dx ThemeProvider resolves after mount). */
const THEME_INIT = `try{var t=localStorage.getItem("dx-theme")||"system";var d=t==="dark"||(t==="system"&&window.matchMedia("(prefers-color-scheme: dark)").matches);document.documentElement.classList.toggle("dark",d);if(localStorage.getItem("dx-accent")==="dx-gradient")document.documentElement.classList.add("accent-dx-gradient");}catch(e){}`;

export default function RootLayout({ children }: LayoutProps<"/">) {
  return (
    <html lang="en" className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: THEME_INIT }} />
      </head>
      <body className="flex min-h-full flex-col bg-background text-foreground">
        <a
          href="#bridge"
          className="sr-only focus:not-sr-only focus:absolute focus:top-2 focus:left-2 focus:z-50 focus:rounded-full focus:bg-primary focus:px-4 focus:py-2 focus:text-sm focus:text-primary-foreground"
        >
          Skip to the bridge
        </a>
        <Providers>{children}</Providers>
      </body>
    </html>
  );
}
