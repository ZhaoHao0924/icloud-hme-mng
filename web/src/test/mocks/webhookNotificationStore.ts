import type { WebhookNotification } from "../../api/schemas";
import { webhookNotificationFixture } from "../fixtures";

export function createMockWebhookNotificationStore() {
  let value = { ...webhookNotificationFixture };

  return {
    get() {
      return { ...value };
    },
    reset() {
      value = { ...webhookNotificationFixture };
    },
    update(input: { enabled: boolean; secret: string; url: string }): WebhookNotification {
      value = {
        ...value,
        configured: value.configured || input.secret.trim() !== "",
        enabled: input.enabled,
        url: input.url.trim(),
      };
      return { ...value };
    },
  };
}

export type MockWebhookNotificationStore = ReturnType<typeof createMockWebhookNotificationStore>;
