import { createContext, useContext } from "react";

export type ApiTokenContextValue = {
  clearApiToken: () => void;
  hasApiToken: boolean;
  openApiTokenDialog: () => void;
};

export const ApiTokenContext = createContext<ApiTokenContextValue | null>(null);

export function useApiToken() {
  const context = useContext(ApiTokenContext);
  if (!context) {
    throw new Error("useApiToken must be used within ApiTokenProvider");
  }
  return context;
}
