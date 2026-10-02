import {
  notificationClassVariants,
  type ChannelPreference,
  type NotificationClass,
  type NotificationDetailLevel,
  type NotificationPreferences,
} from "../api/generated/index.ts";

export const NOTIFICATION_CHANNELS = ["push", "email", "in_app"] as const;
export type NotificationChannel = (typeof NOTIFICATION_CHANNELS)[number];

export const NON_SUPPRESSIBLE_NOTIFICATION_CLASSES = new Set<NotificationClass>([
  "security-recovery",
  "security-wallet-rebinding",
]);

function cloneChannel(channel: ChannelPreference): ChannelPreference {
  return {
    enabled: channel.enabled,
    classes: channel.classes.map((entry) => ({ ...entry })),
  };
}

export function cloneNotificationPreferences(
  preferences: NotificationPreferences,
): NotificationPreferences {
  return {
    push: cloneChannel(preferences.push),
    email: cloneChannel(preferences.email),
    in_app: cloneChannel(preferences.in_app),
    detail: preferences.detail,
  };
}

export function classEnabled(
  preferences: NotificationPreferences,
  channel: NotificationChannel,
  notificationClass: NotificationClass,
): boolean {
  const preference = preferences[channel];
  return preference.enabled
    && preference.classes.some((entry) => entry.class === notificationClass && entry.enabled);
}

export function fullySuppressesSecurityClass(preferences: NotificationPreferences): boolean {
  return [...NON_SUPPRESSIBLE_NOTIFICATION_CLASSES].some((notificationClass) =>
    NOTIFICATION_CHANNELS.every((channel) => !classEnabled(preferences, channel, notificationClass))
  );
}

export function withChannelEnabled(
  preferences: NotificationPreferences,
  channel: NotificationChannel,
  enabled: boolean,
): NotificationPreferences | undefined {
  const candidate = cloneNotificationPreferences(preferences);
  candidate[channel].enabled = enabled;
  return fullySuppressesSecurityClass(candidate) ? undefined : candidate;
}

export function withClassEnabled(
  preferences: NotificationPreferences,
  channel: NotificationChannel,
  notificationClass: NotificationClass,
  enabled: boolean,
): NotificationPreferences | undefined {
  const candidate = cloneNotificationPreferences(preferences);
  const current = candidate[channel].classes.find((entry) => entry.class === notificationClass);
  if (current === undefined) {
    candidate[channel].classes.push({ class: notificationClass, enabled });
  } else {
    current.enabled = enabled;
  }
  return fullySuppressesSecurityClass(candidate) ? undefined : candidate;
}

export function withDetailLevel(
  preferences: NotificationPreferences,
  detail: NotificationDetailLevel,
): NotificationPreferences {
  return { ...cloneNotificationPreferences(preferences), detail };
}

export function normalizedNotificationClasses(
  channel: ChannelPreference,
): readonly Readonly<{ class: NotificationClass; enabled: boolean }>[] {
  return notificationClassVariants.map((notificationClass) => ({
    class: notificationClass,
    enabled: channel.classes.find((entry) => entry.class === notificationClass)?.enabled ?? false,
  }));
}

export function settings(): Readonly<{
  channels: typeof NOTIFICATION_CHANNELS;
  nonSuppressible: ReadonlySet<NotificationClass>;
}> {
  return Object.freeze({
    channels: NOTIFICATION_CHANNELS,
    nonSuppressible: NON_SUPPRESSIBLE_NOTIFICATION_CLASSES,
  });
}

export function human_web_settings() {
  return settings();
}
