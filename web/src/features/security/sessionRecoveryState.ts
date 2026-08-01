const sessionExpiredReason = "icloud_session_expired";

export type SessionRecoveryRequest = {
  from: string;
  reason: typeof sessionExpiredReason;
};

export type SessionRecoveryLocationState = {
  sessionRecovery: SessionRecoveryRequest;
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isSafeInternalPath(value: string) {
  return value.startsWith("/") && !value.startsWith("//");
}

export function createSessionRecoveryLocationState(from: string): SessionRecoveryLocationState {
  return {
    sessionRecovery: { from, reason: sessionExpiredReason },
  };
}

export function readSessionRecoveryLocationState(value: unknown): SessionRecoveryRequest | null {
  if (!isRecord(value) || !isRecord(value.sessionRecovery)) return null;
  const { from, reason } = value.sessionRecovery;
  if (reason !== sessionExpiredReason || typeof from !== "string" || !isSafeInternalPath(from)) {
    return null;
  }
  return { from, reason };
}

export function isStoredSessionExpiredError(message: string) {
  const normalized = message.trim().toLowerCase();
  if (normalized === "") return false;
  if (
    normalized.includes("会话失效") ||
    normalized.includes("会话已过期") ||
    normalized.includes("session expired")
  ) {
    return true;
  }
  return normalized.includes("cookie") && /(^|\D)(401|403)(\D|$)/.test(normalized);
}
