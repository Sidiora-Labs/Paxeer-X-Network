export const notification = {
  // Announcement related keys
  "notification.campaign": "Campaign announcement",
  "notification.delisting": "Token delisting",
  "notification.general": "General update",
  "notification.joinNow": "Join now",
  "notification.listing": "New token listing",
  "notification.vote": "Community vote",
  "notification.maintenance": "System maintenance",
  "notification.maintenanceDuration.hours":
    "{{hours}} HRs at {{startTimeFormatted}} - {{endTimeFormatted}} (UTC)",
  "notification.maintenanceDuration.minutes":
    "{{minutes}} MINs at {{startTimeFormatted}} - {{endTimeFormatted}} (UTC)",
  "notification.recentlyUpdated": "Recently updated",
  "notification.title": "Announcement",
  "notification.centerTitle": "Announcement center",

  // Notification related keys
  "notification.closeAll": "Close all ({{total}})",
  "notification.delistingTitle": "Token Delisting Notice",
  "notification.generalTitle": "General Update",
  "notification.maintenanceTitle": "System Maintenance",
  "notification.empty": "No announcements",
};

export type Notification = typeof notification;
