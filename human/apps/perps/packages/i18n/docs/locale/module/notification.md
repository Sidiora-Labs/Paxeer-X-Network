# notification.ts

## notification.ts responsibility

Provides announcement and notification copy: campaign, delisting, general, join now, listing, vote, maintenance, maintenance duration (hours/minutes), recently updated, title, center title, close all (with total), delisting/general/maintenance titles, empty announcements.

## notification.ts exports

| Name | Type | Role | Description |
|------|------|------|-------------|
| notification | object | Key-value map | Keys under "notification.*" |
| Notification | type | typeof notification | Type export |

## notification.ts Example

```typescript
t("notification.title");
t("notification.maintenanceDuration.hours");
t("notification.empty");
```
