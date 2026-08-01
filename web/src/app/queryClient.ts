import { QueryClient } from "@tanstack/react-query";

import { shouldRetryApiRequest } from "../api/client";

export function createQueryClient() {
  return new QueryClient({
    defaultOptions: {
      mutations: {
        retry: false,
      },
      queries: {
        gcTime: 5 * 60 * 1000,
        refetchOnWindowFocus: false,
        retry: shouldRetryApiRequest,
        staleTime: 30 * 1000,
      },
    },
  });
}

export const queryClient = createQueryClient();
