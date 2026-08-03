import { createContext, useContext } from "react";

import type { PlatformAuthStatus } from "../api/schemas";

export type PlatformAuthContextValue = {
  error: unknown;
  isLoading: boolean;
  isLoggingOut: boolean;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
  setAuthenticated: (status: PlatformAuthStatus) => void;
  status: PlatformAuthStatus | undefined;
};

export const PlatformAuthContext = createContext<PlatformAuthContextValue | null>(null);

export function usePlatformAuth() {
  const context = useContext(PlatformAuthContext);
  if (!context) {
    throw new Error("usePlatformAuth must be used within PlatformAuthProvider");
  }
  return context;
}
