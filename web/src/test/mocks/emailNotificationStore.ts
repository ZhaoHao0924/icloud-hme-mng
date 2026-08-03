import type { EmailNotification } from "../../api/schemas";
import { emailNotificationFixture } from "../fixtures";

export function createMockEmailNotificationStore() {
  let value = { ...emailNotificationFixture };

  return {
    get() {
      return { ...value };
    },
    reset() {
      value = { ...emailNotificationFixture };
    },
    update(input: {
      authorizationCode: string;
      enabled: boolean;
      senderEmail: string;
      recipientEmail: string;
    }): EmailNotification {
      value = {
        ...value,
        configured: value.configured || input.authorizationCode.trim() !== "",
        enabled: input.enabled,
        sender_email: input.senderEmail.trim(),
        recipient_email: input.recipientEmail.trim(),
      };
      return { ...value };
    },
  };
}

export type MockEmailNotificationStore = ReturnType<typeof createMockEmailNotificationStore>;
