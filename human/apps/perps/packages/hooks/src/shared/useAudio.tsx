import { useCallback, useEffect, useRef } from "react";

let orderFilledAudio: HTMLAudioElement | null = null;

function getOrderFilledAudio(): HTMLAudioElement {
  if (!orderFilledAudio) {
    orderFilledAudio = new Audio();
  }
  return orderFilledAudio;
}

export interface AudioPlayerOptions {
  volume?: number;
  /** When true, play() will run; when false, play() no-ops. Used for on/off toggle. */
  enabled?: boolean;
}

/**
 * Single shared Audio instance. Play is explicit: pause() then set src then play().
 * Use for order-filled notification sound (and any other one-shot global sound).
 * Compatible with legacy single-sound + on/off: pass enabled = user's on/off and src = media or "".
 */
export const useAudioPlayer = (
  src: string,
  options: AudioPlayerOptions = {},
) => {
  const { volume = 1, enabled = true } = options;
  const srcRef = useRef(src);
  const enabledRef = useRef(enabled);
  const volumeRef = useRef(volume);

  useEffect(() => {
    srcRef.current = src;
    enabledRef.current = enabled;
    volumeRef.current = volume;
  }, [src, enabled, volume]);

  useEffect(() => {
    const el = getOrderFilledAudio();
    el.volume = Math.max(0, Math.min(1, volume));
  }, [volume]);

  // Reset audio element when tab becomes visible to prevent
  // accumulated sound replays from queued play() calls in non-Chrome browsers
  useEffect(() => {
    const handleVisibilityChange = () => {
      if (document.visibilityState === "visible") {
        const el = getOrderFilledAudio();
        el.pause();
        el.removeAttribute("src");
        el.load();
      }
    };

    document.addEventListener("visibilitychange", handleVisibilityChange);
    return () => {
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, []);

  const play = useCallback(() => {
    const currentSrc = srcRef.current;
    const currentEnabled = enabledRef.current;
    if (!currentEnabled || !currentSrc) return;
    const el = getOrderFilledAudio();
    el.pause();
    el.src = currentSrc;
    el.volume = Math.max(0, Math.min(1, volumeRef.current));
    el.play()?.catch(() => {});
  }, []);

  return { play };
};
