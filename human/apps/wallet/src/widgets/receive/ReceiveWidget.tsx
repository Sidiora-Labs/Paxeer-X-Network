'use client';

import { useState } from 'react';
import { motion, AnimatePresence } from 'framer-motion';
import { Copy } from 'lucide-react';
import { useWalletState } from '@/providers/WalletProvider';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { QRCodeSVG } from 'qrcode.react';
import { useLocale } from '@/providers/LocaleProvider';

interface ReceiveWidgetProps {
  onBack: () => void;
}

export function ReceiveWidget({ onBack }: ReceiveWidgetProps) {
  const { activeAccount } = useWalletState();
  const { t } = useLocale();
  const [copied, setCopied] = useState(false);
  const address = activeAccount?.address || '';

  const handleCopy = async () => {
    await navigator.clipboard.writeText(address);
    setCopied(true);
    setTimeout(() => setCopied(false), 2200);
  };

  const handleShare = async () => {
    if (navigator.share) {
      await navigator.share({ title: t.receive.shareTitle, text: address });
    } else {
      handleCopy();
    }
  };

  return (
    <div className="min-h-screen flex flex-col px-4 pt-4 safe-area-pt">
      {/* Copy toast */}
      <AnimatePresence>
        {copied && (
          <motion.div
            initial={{ opacity: 0, y: -10, scale: 0.95 }}
            animate={{ opacity: 1, y: 0, scale: 1 }}
            exit={{ opacity: 0, y: -8, scale: 0.95 }}
            transition={{ duration: 0.22 }}
            className="fixed top-[calc(env(safe-area-inset-top,0px)+66px)] left-1/2 -translate-x-1/2 z-50"
          >
            <div className="flex items-center gap-2 px-4 py-2.5 rounded-full bg-pax-card   shadow-2xl">
              <Copy className="w-3.5 h-3.5 text-green-400" />
              <span className="text-xs font-medium text-green-400">{t.receive.addressCopied}</span>
            </div>
          </motion.div>
        )}
      </AnimatePresence>

      <div className="flex items-center gap-3 mb-6">
        <button type="button" onClick={onBack} aria-label="Back" className="p-2 -ml-2 press-scale">
          <SvgIcon name="arrow-left" className="w-5 h-5" style={{ filter: 'brightness(0) invert(0.6)' }} />
        </button>
        <h2 className="text-lg font-bold">{t.receive.title}</h2>
      </div>

      <div className="flex-1 flex flex-col items-center justify-center gap-6 pb-20">
        <div className="glass-card p-6 rounded-3xl">
          <div className="bg-white p-4 rounded-2xl">
            <QRCodeSVG
              value={address}
              size={200}
              level="H"
              bgColor="var(--color-white, #ffffff)"
              fgColor="var(--color-gray-900, #000000)"
            />
          </div>
        </div>

        <div className="text-center">
          <p className="text-xs text-pax-muted mb-1">{t.receive.yourAddress}</p>
          <p className="text-sm font-mono text-white/80 break-all max-w-xs px-4">
            {address}
          </p>
        </div>

        <div className="flex gap-3">
          <button
            onClick={handleCopy}
            className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-white/5 text-sm press-scale"
            aria-label={t.common.copy}
          >
            {copied
              ? <SvgIcon name="check" className="w-4 h-4" style={{ filter: 'invert(69%) sepia(61%) saturate(588%) hue-rotate(88deg) brightness(93%) contrast(93%)' }} />
              : <SvgIcon name="copy" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />}
            {copied ? t.common.copied : t.common.copy}
          </button>
          <button
            onClick={handleShare}
            className="flex items-center gap-2 px-5 py-2.5 rounded-xl bg-white/5 text-sm press-scale"
            aria-label={t.receive.share}
          >
            <SvgIcon name="share" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />
            {t.receive.share}
          </button>
        </div>
      </div>
    </div>
  );
}
