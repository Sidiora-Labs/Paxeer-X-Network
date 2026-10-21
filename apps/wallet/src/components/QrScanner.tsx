'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { ImagePlus, RotateCcw, SwitchCamera } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { useLocale } from '@/providers/LocaleProvider';

interface QrScannerProps {
  open: boolean;
  onClose: () => void;
  onScan: (data: string) => void;
}

export function QrScanner({ open, onClose, onScan }: QrScannerProps) {
  const { p, t } = useLocale();
  const scannerRef = useRef<any>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const [error, setError] = useState('');
  const [facingMode, setFacingMode] = useState<'environment' | 'user'>('environment');
  const [restartToken, setRestartToken] = useState(0);
  const hasScannedRef = useRef(false);

  // Stable refs so the useEffect never restarts due to callback identity changes
  const onScanRef = useRef(onScan);
  const onCloseRef = useRef(onClose);
  useEffect(() => { onScanRef.current = onScan; }, [onScan]);
  useEffect(() => { onCloseRef.current = onClose; }, [onClose]);

  const stopScanner = useCallback(async () => {
    if (scannerRef.current) {
      try {
        const state = scannerRef.current.getState();
        if (state === 2) {
          await scannerRef.current.stop();
        }
        scannerRef.current.clear();
      } catch {
        // Scanner may already be stopped
      }
      scannerRef.current = null;
    }
  }, []);

  // Focus management
  useEffect(() => {
    if (open) {
      previousFocusRef.current = document.activeElement as HTMLElement;
      requestAnimationFrame(() => {
        dialogRef.current?.focus();
      });
    } else if (previousFocusRef.current) {
      previousFocusRef.current.focus();
      previousFocusRef.current = null;
    }
  }, [open]);

  // Escape key
  useEffect(() => {
    if (!open) return;
    const handleKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onCloseRef.current();
    };
    document.addEventListener('keydown', handleKey);
    return () => document.removeEventListener('keydown', handleKey);
  }, [open]);

  useEffect(() => {
    if (!open) {
      stopScanner();
      hasScannedRef.current = false;
      setError('');
      return;
    }

    let cancelled = false;

    const startScanner = async () => {
      const { Html5Qrcode } = await import('html5-qrcode');

      if (cancelled) return;

      await stopScanner();

      const elementId = 'qr-scanner-region';
      if (!document.getElementById(elementId)) return;

      const scanner = new Html5Qrcode(elementId, { verbose: false });
      scannerRef.current = scanner;

      try {
        await scanner.start(
          { facingMode },
          {
            fps: 15,
            qrbox: (viewfinderWidth: number, viewfinderHeight: number) => {
              const side = Math.min(viewfinderWidth, viewfinderHeight) * 0.7;
              return { width: Math.floor(side), height: Math.floor(side) };
            },
            disableFlip: false,
          },
          (decodedText: string) => {
            if (hasScannedRef.current) return;
            hasScannedRef.current = true;
            onScanRef.current(decodedText);
            onCloseRef.current();
          },
          () => {
            // QR code not found in frame
          },
        );
        setError('');
      } catch (err: any) {
        if (!cancelled) {
          const msg = typeof err === 'string' ? err : err?.message || 'Camera access denied';
          if (msg.includes('NotAllowedError') || msg.includes('Permission')) {
            setError(p.cameraDenied);
          } else if (msg.includes('NotFoundError') || msg.includes('no camera')) {
            setError(p.cameraMissing);
          } else {
            setError(msg);
          }
        }
      }
    };

    const timer = setTimeout(startScanner, 150);

    return () => {
      cancelled = true;
      clearTimeout(timer);
      stopScanner();
    };
  }, [
    open,
    facingMode,
    restartToken,
    stopScanner,
    p.cameraDenied,
    p.cameraMissing,
  ]);

  const handleFlipCamera = () => {
    setFacingMode((prev) => (prev === 'environment' ? 'user' : 'environment'));
  };

  const handleRetry = () => {
    setError('');
    hasScannedRef.current = false;
    setRestartToken((value) => value + 1);
  };

  const handleFileImport = useCallback(async () => {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/*';
    input.onchange = async () => {
      const file = input.files?.[0];
      if (!file) return;
      try {
        const { Html5Qrcode } = await import('html5-qrcode');
        const scanner = new Html5Qrcode('qr-file-scan', { verbose: false });
        const result = await scanner.scanFile(file, /* showImage= */ false);
        onScanRef.current(result);
        onCloseRef.current();
      } catch {
        setError(p.qrNotFound);
      }
    };
    input.click();
  }, [p.qrNotFound]);

  if (!open) return null;

  return (
    <div ref={dialogRef} role="dialog" aria-modal="true" aria-label={p.scanQr} tabIndex={-1}
      className="fixed inset-0 z-[60] bg-black flex flex-col outline-none">
      {/* Hidden container for file-based QR scanning */}
      <div id="qr-file-scan" className="hidden" />

      {/* Header */}
      <div className="relative z-10 flex items-center justify-between px-4 pt-4 safe-area-pt">
        <button
          onClick={onClose}
          aria-label={t.common.close}
          className="p-2.5 rounded-full bg-white/10 backdrop-blur-sm press-scale"
        >
          <SvgIcon name="x" className="w-5 h-5" style={{ filter: 'brightness(0) invert(1)' }} />
        </button>
        <h2 className="text-sm font-semibold text-white">{p.scanQr}</h2>
        <div className="flex gap-2">
          <button
            onClick={handleFileImport}
            aria-label={p.importQr}
            className="p-2.5 rounded-full bg-white/10 backdrop-blur-sm press-scale"
          >
            <ImagePlus className="w-5 h-5 text-white" />
          </button>
          <button
            onClick={handleFlipCamera}
            aria-label={p.switchCamera}
            className="p-2.5 rounded-full bg-white/10 backdrop-blur-sm press-scale"
          >
            <SwitchCamera className="w-5 h-5 text-white" />
          </button>
        </div>
      </div>

      {/* Scanner viewport */}
      <div className="flex-1 flex items-center justify-center" ref={containerRef}>
        <div className="relative w-full max-w-sm mx-4">
          <div id="qr-scanner-region" className="rounded-2xl" />
        </div>
      </div>

      {/* Instructions / Error */}
      <div className="px-6 pb-8 safe-area-pb text-center">
        {error ? (
          <div className="bg-red-500/10 rounded-xl px-4 py-3 space-y-2">
            <p className="text-xs text-red-400">{error}</p>
            <button onClick={handleRetry} className="inline-flex items-center gap-1.5 text-xs text-white/70 hover:text-white">
              <RotateCcw className="w-3.5 h-3.5" />
              {t.common.retry}
            </button>
          </div>
        ) : (
          <p className="text-xs text-white/50">
            {p.scanInstruction}
          </p>
        )}
      </div>
    </div>
  );
}
