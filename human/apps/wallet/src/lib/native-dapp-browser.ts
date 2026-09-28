// ── Native DApp Browser Bridge ──────────────────────────────────────────────
// TypeScript wrapper for the custom DAppBrowserPlugin (Android native WebView).
// Only activates when running inside Capacitor. Falls back to noop in browser.

import { Capacitor, registerPlugin } from '@capacitor/core';
import type { PluginListenerHandle } from '@capacitor/core';

interface DAppBrowserPluginInterface {
  open(options: {
    url: string;
    title?: string;
    address?: string;
    chainId?: string;
    rpcUrl?: string;
  }): Promise<void>;
  close(): Promise<void>;
  sendRpcResponse(options: {
    id: string;
    result?: string;
    error?: string;
  }): Promise<void>;
  pushEvent(options: {
    event: string;
    payload?: string;
  }): Promise<void>;
  addListener(
    eventName: 'rpcRequest',
    handler: (data: NativeRpcRequest) => void,
  ): Promise<PluginListenerHandle>;
  addListener(
    eventName: 'browserClosed',
    handler: () => void,
  ): Promise<PluginListenerHandle>;
}

export interface NativeRpcRequest {
  id: string;
  method: string;
  params: string; // JSON-stringified
  origin: string;
}

const DAppBrowserNative = Capacitor.isNativePlatform()
  ? registerPlugin<DAppBrowserPluginInterface>('DAppBrowser')
  : null;

export function isNativeDAppBrowserAvailable(): boolean {
  return Capacitor.isNativePlatform() && DAppBrowserNative !== null;
}

export async function openNativeDAppBrowser(options: {
  url: string;
  title?: string;
  address?: string;
  chainId?: string;
  rpcUrl?: string;
}): Promise<void> {
  if (!DAppBrowserNative) return;
  await DAppBrowserNative.open(options);
}

export async function closeNativeDAppBrowser(): Promise<void> {
  if (!DAppBrowserNative) return;
  await DAppBrowserNative.close();
}

export async function sendNativeRpcResponse(
  id: string,
  result?: unknown,
  error?: { code: number; message: string },
): Promise<void> {
  if (!DAppBrowserNative) return;
  await DAppBrowserNative.sendRpcResponse({
    id,
    result: result !== undefined ? JSON.stringify(result) : undefined,
    error: error ? JSON.stringify(error) : undefined,
  });
}

export async function pushNativeEvent(
  event: string,
  payload?: unknown,
): Promise<void> {
  if (!DAppBrowserNative) return;
  await DAppBrowserNative.pushEvent({
    event,
    payload: payload !== undefined ? JSON.stringify(payload) : undefined,
  });
}

export function onNativeRpcRequest(
  handler: (data: NativeRpcRequest) => void,
): Promise<PluginListenerHandle> | null {
  if (!DAppBrowserNative) return null;
  return DAppBrowserNative.addListener('rpcRequest', handler);
}

export function onNativeBrowserClosed(
  handler: () => void,
): Promise<PluginListenerHandle> | null {
  if (!DAppBrowserNative) return null;
  return DAppBrowserNative.addListener('browserClosed', handler);
}
