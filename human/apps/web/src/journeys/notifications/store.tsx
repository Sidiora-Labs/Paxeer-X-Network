"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from "react";

import { humanApi, type HumanApiClient } from "../../api";
import { Notifications, type NotificationLanding } from "./controller";
import { unreadNotificationCount, type PresentedNotification } from "./model";

export type NotificationCenterState =
  | Readonly<{ status: "loading"; notifications: readonly []; unreadCount: 0; approvalCount: 0 }>
  | Readonly<{ status: "error"; notifications: readonly PresentedNotification[]; unreadCount: number; approvalCount: number; error: unknown }>
  | Readonly<{
      status: "ready";
      notifications: readonly PresentedNotification[];
      unreadCount: number;
      approvalCount: number;
    }>;

interface NotificationCenterValue {
  readonly state: NotificationCenterState;
  readonly refresh: () => Promise<void>;
  readonly open: (notification: PresentedNotification) => Promise<NotificationLanding>;
}

const NotificationCenterContext = createContext<NotificationCenterValue | undefined>(undefined);

export function NotificationCenterProvider({
  children,
  client: suppliedClient,
}: Readonly<{ children: ReactNode; client?: HumanApiClient }>) {
  const client = useMemo(() => suppliedClient ?? humanApi(), [suppliedClient]);
  const notifications = useMemo(() => new Notifications({ client }), [client]);
  const [state, setState] = useState<NotificationCenterState>({
    status: "loading",
    notifications: [],
    unreadCount: 0,
    approvalCount: 0,
  });

  const refresh = useCallback(async () => {
    try {
      const [archive, approvalCount] = await Promise.all([
        notifications.archive(),
        notifications.pendingApprovals(),
      ]);
      setState({
        status: "ready",
        notifications: archive,
        unreadCount: unreadNotificationCount(archive),
        approvalCount,
      });
    } catch (error) {
      setState((current) => ({ ...current, status: "error", error }));
    }
  }, [notifications]);

  useEffect(() => {
    void refresh();
    const onFocus = () => { void refresh(); };
    const onOnline = () => { void refresh(); };
    const interval = window.setInterval(onFocus, 30_000);
    window.addEventListener("focus", onFocus);
    window.addEventListener("online", onOnline);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener("focus", onFocus);
      window.removeEventListener("online", onOnline);
    };
  }, [refresh]);

  const open = useCallback(async (notification: PresentedNotification) => {
    const landing = await notifications.open(notification);
    setState((current) => {
      if (current.status !== "ready") {
        return current;
      }
      const next = current.notifications.map((item) => item.source.notification_id === landing.notification.notification_id
        ? Object.freeze({ ...item, source: landing.notification })
        : item);
      return {
        ...current,
        notifications: next,
        unreadCount: unreadNotificationCount(next),
      };
    });
    void refresh();
    return landing;
  }, [notifications, refresh]);

  const value = useMemo(() => ({ state, refresh, open }), [state, refresh, open]);
  return (
    <NotificationCenterContext.Provider value={value}>
      {children}
    </NotificationCenterContext.Provider>
  );
}

export function useNotificationCenter(): NotificationCenterValue {
  const value = useContext(NotificationCenterContext);
  if (value === undefined) {
    throw new Error("NotificationCenterProvider is required on authenticated surfaces");
  }
  return value;
}
