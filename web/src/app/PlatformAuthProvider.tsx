import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useMemo, useState, type ReactNode } from "react";

import { api, isPlatformAuthError } from "../api/client";
import { platformAuthQueryOptions, queryKeys } from "../api/queries";
import type { PlatformAuthStatus } from "../api/schemas";
import { PlatformAuthContext } from "./platformAuthContext";

type PlatformAuthProviderProps = {
  children: ReactNode;
};

function unauthenticatedStatus(configured: boolean): PlatformAuthStatus {
  return {
    authenticated: false,
    configured,
    expires_at: "",
    username: "",
  };
}

export function PlatformAuthProvider({ children }: PlatformAuthProviderProps) {
  const queryClient = useQueryClient();
  const sessionQuery = useQuery(platformAuthQueryOptions());
  const [isLoggingOut, setIsLoggingOut] = useState(false);

  const setUnauthenticated = useCallback(
    (configured: boolean) => {
      void queryClient.cancelQueries();
      queryClient.clear();
      queryClient.setQueryData(queryKeys.platformAuth, unauthenticatedStatus(configured));
    },
    [queryClient],
  );

  useEffect(() => {
    const handleError = (error: unknown) => {
      if (!isPlatformAuthError(error)) return;
      const configured = (error as { code?: string }).code !== "platform_auth_setup_required";
      setUnauthenticated(configured);
    };
    const unsubscribeQueries = queryClient.getQueryCache().subscribe((event) => {
      if (event.type === "updated") {
        handleError(event.query.state.error);
      }
    });
    const unsubscribeMutations = queryClient.getMutationCache().subscribe((event) => {
      if (event.type === "updated") {
        handleError(event.mutation.state.error);
      }
    });

    return () => {
      unsubscribeQueries();
      unsubscribeMutations();
    };
  }, [queryClient, setUnauthenticated]);

  const setAuthenticated = useCallback(
    (status: PlatformAuthStatus) => {
      queryClient.setQueryData(queryKeys.platformAuth, status);
    },
    [queryClient],
  );

  const refresh = useCallback(async () => {
    await sessionQuery.refetch();
  }, [sessionQuery]);

  const logout = useCallback(async () => {
    if (isLoggingOut) return;
    setIsLoggingOut(true);
    try {
      await api.logoutPlatform();
    } finally {
      setUnauthenticated(sessionQuery.data?.configured ?? true);
      setIsLoggingOut(false);
    }
  }, [isLoggingOut, sessionQuery.data?.configured, setUnauthenticated]);

  const contextValue = useMemo(
    () => ({
      error: sessionQuery.error,
      isLoading: sessionQuery.isPending,
      isLoggingOut,
      logout,
      refresh,
      setAuthenticated,
      status: sessionQuery.data,
    }),
    [
      isLoggingOut,
      logout,
      refresh,
      sessionQuery.data,
      sessionQuery.error,
      sessionQuery.isPending,
      setAuthenticated,
    ],
  );

  return (
    <PlatformAuthContext.Provider value={contextValue}>{children}</PlatformAuthContext.Provider>
  );
}
