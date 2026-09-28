'use client';

import { useState, useEffect } from 'react';
import { Bell, BellOff, WifiOff, RefreshCw } from 'lucide-react';
import { SvgIcon } from '@/components/ui/SvgIcon';
import { usePWA } from '@/providers/PWAProvider';

// ── Install Banner ──────────────────────────────────────────────────────────
// Shown as a slide-up banner when the app is installable

export function InstallBanner() {
  const { showInstallPrompt, promptInstall, dismissInstallPrompt, isStandalone } = usePWA();

  if (!showInstallPrompt || isStandalone) return null;

  const isIOS = /iPad|iPhone|iPod/.test(navigator.userAgent);

  return (
    <div className="fixed bottom-24 left-3 right-3 z-50 animate-slide-up">
      <div className="glass-card   p-4 shadow-2xl shadow-black/40">
        <button
          onClick={dismissInstallPrompt}
          className="absolute top-3 right-3 p-1 text-pax-muted press-scale"
        >
          <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />
        </button>

        <div className="flex items-start gap-3">
          <div className="w-11 h-11 rounded-xl bg-pax-accent/10 flex items-center justify-center shrink-0">
            <SvgIcon name="arrow-left" className="w-5 h-5 rotate-90" style={{ filter: 'brightness(0) saturate(100%) invert(22%) sepia(93%) saturate(7388%) hue-rotate(222deg) brightness(98%) contrast(101%)' }} />
          </div>
          <div className="flex-1 min-w-0 pr-4">
            <p className="text-sm font-semibold">Install Paxeer Wallet</p>
            <p className="text-xs text-pax-muted mt-0.5">
              {isIOS
                ? 'Tap the share button, then "Add to Home Screen"'
                : 'Add to your home screen for the best experience'}
            </p>
          </div>
        </div>

        {!isIOS ? (
          <button
            onClick={promptInstall}
            className="mt-3 w-full py-2.5 rounded-xl bg-pax-accent text-black text-sm font-semibold press-scale transition-colors hover:bg-pax-accent/90"
          >
            Install App
          </button>
        ) : (
          <div className="mt-3 flex items-center gap-2 text-xs text-pax-muted">
            <SvgIcon name="share" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(1)' }} />
            <span>Tap <strong className="text-white">Share</strong> → <strong className="text-white">Add to Home Screen</strong></span>
          </div>
        )}
      </div>
    </div>
  );
}

// ── Notification Prompt ─────────────────────────────────────────────────────
// Shown as a toast-like banner asking to enable push notifications

export function NotificationPrompt() {
  const {
    showNotificationPrompt,
    subscribePush,
    dismissNotificationPrompt,
    notificationPermission,
  } = usePWA();
  const [error, setError] = useState<string | null>(null);
  const [subscribing, setSubscribing] = useState(false);

  if (!showNotificationPrompt || notificationPermission !== 'default') return null;

  const handleAllow = async () => {
    setError(null);
    setSubscribing(true);
    try {
      const subscription = await subscribePush();
      if (!subscription && Notification.permission === 'granted') {
        setError('Notifications were allowed, but push setup failed. Please retry.');
      }
    } catch {
      setError('Notifications were allowed, but push setup failed. Please retry.');
    } finally {
      setSubscribing(false);
    }
  };

  return (
    <div className="fixed top-[calc(env(safe-area-inset-top,0px)+60px)] left-3 right-3 z-50 animate-slide-down">
      <div className="glass-card   p-4 shadow-2xl shadow-black/40">
        <button
          onClick={dismissNotificationPrompt}
          className="absolute top-3 right-3 p-1 text-pax-muted press-scale"
        >
          <SvgIcon name="x" className="w-4 h-4" style={{ filter: 'brightness(0) invert(1)' }} />
        </button>

        <div className="flex items-start gap-3">
          <div className="w-11 h-11 rounded-xl bg-yellow-500/10 flex items-center justify-center shrink-0">
            <Bell className="w-5 h-5 text-yellow-400" />
          </div>
          <div className="flex-1 min-w-0 pr-4">
            <p className="text-sm font-semibold">Enable Notifications</p>
            <p className="text-xs text-pax-muted mt-0.5">
              Get alerts for transactions, price changes & more
            </p>
          </div>
        </div>

        <div className="flex gap-2 mt-3">
          <button
            onClick={dismissNotificationPrompt}
            disabled={subscribing}
            className="flex-1 py-2 rounded-xl bg-white/5 text-sm font-medium text-pax-muted press-scale"
          >
            Not now
          </button>
          <button
            onClick={handleAllow}
            disabled={subscribing}
            className="flex-1 py-2 rounded-xl bg-pax-accent text-black text-sm font-semibold press-scale disabled:opacity-60"
          >
            {subscribing ? 'Enabling…' : 'Enable'}
          </button>
        </div>
        {error && (
          <p className="mt-3 rounded-xl   bg-pax-error/10 px-3 py-2 text-xs font-medium text-pax-error">
            {error}
          </p>
        )}
      </div>
    </div>
  );
}

// ── Floating App Icon ───────────────────────────────────────────────────────
// A small floating button shown when the app is installable but banner is dismissed

export function FloatingAppIcon() {
  const { showFloatingIcon, promptInstall, isStandalone } = usePWA();
  const [expanded, setExpanded] = useState(false);

  if (!showFloatingIcon || isStandalone) return null;

  const isIOS = typeof navigator !== 'undefined' && /iPad|iPhone|iPod/.test(navigator.userAgent);

  const handleClick = () => {
    if (isIOS) {
      setExpanded(!expanded);
    } else {
      promptInstall();
    }
  };

  return (
    <div className="fixed bottom-28 right-4 z-50">
      {expanded && isIOS && (
        <div className="mb-2 glass-card   p-3 rounded-xl text-xs text-pax-muted w-52 animate-fade-in shadow-xl shadow-black/40">
          <div className="flex items-center gap-2">
            <SvgIcon name="share" className="w-3.5 h-3.5" style={{ filter: 'brightness(0) invert(1)' }} />
            <span>
              Tap <strong className="text-white">Share</strong> → <strong className="text-white">Add to Home Screen</strong>
            </span>
          </div>
        </div>
      )}
      <button
        onClick={handleClick}
        className="w-12 h-12 rounded-full bg-pax-accent shadow-lg shadow-pax-accent/25 flex items-center justify-center press-scale animate-bounce-subtle"
        aria-label="Install App"
      >
        <SvgIcon name="apps" className="w-5 h-5" style={{ filter: 'brightness(0)' }} />
      </button>
    </div>
  );
}

// ── Notification Toggle (inline, for use in Settings) ───────────────────────

export function NotificationToggle({ className = '' }: { className?: string }) {
  const {
    pushSupported,
    notificationPermission,
    isSubscribed,
    subscribePush,
    unsubscribePush,
  } = usePWA();
  const [loading, setLoading] = useState(false);

  if (!pushSupported) {
    return (
      <div className={`flex items-center gap-2 text-xs text-pax-muted ${className}`}>
        <BellOff className="w-3.5 h-3.5" />
        <span>Push not supported in this browser</span>
      </div>
    );
  }

  if (notificationPermission === 'denied') {
    return (
      <div className={`flex items-center gap-2 text-xs text-red-400 ${className}`}>
        <BellOff className="w-3.5 h-3.5" />
        <span>Notifications blocked — enable in browser settings</span>
      </div>
    );
  }

  const handleToggle = async () => {
    setLoading(true);
    try {
      if (isSubscribed) {
        await unsubscribePush();
      } else {
        await subscribePush();
      }
    } finally {
      setLoading(false);
    }
  };

  return (
    <button
      onClick={handleToggle}
      disabled={loading}
      className={`flex items-center justify-between w-full py-3 px-4 rounded-xl bg-white/5 press-scale disabled:opacity-50 ${className}`}
    >
      <div className="flex items-center gap-3">
        {isSubscribed ? (
          <Bell className="w-4 h-4 text-pax-accent" />
        ) : (
          <BellOff className="w-4 h-4 text-pax-muted" />
        )}
        <span className="text-sm font-medium">
          {isSubscribed ? 'Notifications enabled' : 'Enable notifications'}
        </span>
      </div>
      <div
        className={`w-10 h-6 rounded-full relative transition-colors ${
          isSubscribed ? 'bg-pax-accent' : 'bg-white/10'
        }`}
      >
        <div
          className={`absolute top-1 w-4 h-4 rounded-full bg-white transition-transform ${
            isSubscribed ? 'left-5' : 'left-1'
          }`}
        />
      </div>
    </button>
  );
}

// ── Update Banner ───────────────────────────────────────────────────────────
// Shown when a new service worker version is detected

export function UpdateBanner() {
  const { updateAvailable, applyUpdate } = usePWA();

  if (!updateAvailable) return null;

  return (
    <div className="fixed top-[calc(env(safe-area-inset-top,0px)+60px)] left-3 right-3 z-50 animate-slide-down">
      <div className="glass-card   p-4 shadow-2xl shadow-black/40">
        <div className="flex items-center gap-3">
          <div className="w-10 h-10 rounded-xl bg-pax-accent/10 flex items-center justify-center shrink-0">
            <RefreshCw className="w-5 h-5 text-pax-accent" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-semibold">Update Available</p>
            <p className="text-xs text-pax-muted mt-0.5">A new version of Paxeer Wallet is ready</p>
          </div>
        </div>
        <button
          onClick={applyUpdate}
          className="mt-3 w-full py-2.5 rounded-xl bg-pax-accent text-white text-sm font-semibold press-scale transition-colors hover:bg-pax-accent/90"
        >
          Refresh Now
        </button>
      </div>
    </div>
  );
}

// ── Offline Indicator ───────────────────────────────────────────────────────
// Shown as a small banner when the device goes offline

export function OfflineIndicator() {
  const [offline, setOffline] = useState(false);

  useEffect(() => {
    const goOffline = () => setOffline(true);
    const goOnline = () => setOffline(false);
    setOffline(!navigator.onLine);
    window.addEventListener('offline', goOffline);
    window.addEventListener('online', goOnline);
    return () => {
      window.removeEventListener('offline', goOffline);
      window.removeEventListener('online', goOnline);
    };
  }, []);

  if (!offline) return null;

  return (
    <div className="fixed bottom-[calc(env(safe-area-inset-bottom,0px)+92px)] left-1/2 z-[80] -translate-x-1/2 animate-fade-in pointer-events-none">
      <div className="flex items-center gap-2 px-4 py-2 rounded-full   bg-pax-warning text-black text-xs font-semibold shadow-2xl shadow-black/40">
        <WifiOff className="w-3.5 h-3.5" />
        <span>No connection</span>
      </div>
    </div>
  );
}
