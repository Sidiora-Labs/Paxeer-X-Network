import { loadProgramApprovals } from "../approvals/controller";
import { programApprovalRoute } from "../approvals/model";
import type {
  HumanApiClient,
  NotificationPage,
  NotificationSummary,
} from "../../api";
import {
  NOTIFICATIONS_ROUTE,
  presentedNotifications,
  safeDeepLink,
  type PresentedNotification,
} from "./model";

export interface NotificationsOptions {
  readonly client: HumanApiClient;
}

export interface NotificationLanding {
  readonly notification: NotificationSummary;
  readonly href: string;
}

export class Notifications {
  readonly #client: HumanApiClient;

  constructor(options: NotificationsOptions) {
    this.#client = options.client;
  }

  async page(): Promise<NotificationPage> {
    return this.#client.notificationList();
  }

  async archive(): Promise<readonly PresentedNotification[]> {
    return presentedNotifications(await this.page());
  }

  async pendingApprovals(): Promise<number> {
    const [page, programs] = await Promise.all([this.#client.approvalList(), loadProgramApprovals(this.#client)]);
    const now = Date.now();
    return [...page.approvals, ...programs].filter((approval) => {
      const expiry = Date.parse(approval.expires_at);
      return approval.state === "pending" && Number.isFinite(expiry) && expiry > now;
    }).length;
  }

  async open(notification: PresentedNotification): Promise<NotificationLanding> {
    const updated = await this.#client.notificationRead(notification.source.notification_id);
    if (updated.approval_id !== undefined) {
      const deepLink = safeDeepLink(updated.deep_link);
      if (deepLink !== undefined) {
        const link = new URL(deepLink, "https://layerx.invalid");
        if (link.searchParams.get("kind") === "program"
          && link.pathname === `/app/approvals/${encodeURIComponent(updated.approval_id)}`) {
          const approval = await this.#client.approvalProgramGet(updated.approval_id);
          if (approval.approval_id !== updated.approval_id) throw new TypeError("Programs notification identity mismatch");
          return Object.freeze({ notification: updated, href: programApprovalRoute(approval.approval_id) });
        }
      }
      const approval = await this.#client.approvalGet(updated.approval_id);
      return Object.freeze({
        notification: updated,
        href: `/app/approvals/${encodeURIComponent(approval.approval_id)}`,
      });
    }
    if (updated.journey_id !== undefined) {
      await this.#client.journeyGet(updated.journey_id);
    }
    return Object.freeze({
      notification: updated,
      href: safeDeepLink(updated.deep_link) ?? NOTIFICATIONS_ROUTE,
    });
  }
}
