declare global {
  interface Window {
    __PAXEER_NETWORK_VERSION__?: {
      [key: string]: string;
    };
    debugPrint: (message: any) => void;
  }
}

interface QRiousStatic {
  new (options?: QRiousOptions): QRiousStatic;
  toDataURL(mime?: string): string;
  canvas: HTMLCanvasElement;
}

type CorrectionLevel = "L" | "M" | "Q" | "H";

interface QRiousOptions {
  background?: string;
  backgroundAlpha?: number;
  foreground?: string;
  foregroundAlpha?: number;
  level?: CorrectionLevel;
  padding?: number;
  size?: number;
  value?: string;
}

declare module "qrious" {
  export = QRious;
}

declare var QRious: QRiousStatic;

// export {};
