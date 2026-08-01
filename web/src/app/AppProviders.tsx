import { QueryClientProvider, type QueryClient } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { ApiTokenProvider } from "./ApiTokenProvider";
import { queryClient as defaultQueryClient } from "./queryClient";
import { NotificationProvider } from "../components/NotificationProvider";

type AppProvidersProps = {
  children: ReactNode;
  queryClient?: QueryClient;
};

export function AppProviders({ children, queryClient = defaultQueryClient }: AppProvidersProps) {
  return (
    <QueryClientProvider client={queryClient}>
      <ApiTokenProvider>
        <NotificationProvider>{children}</NotificationProvider>
      </ApiTokenProvider>
    </QueryClientProvider>
  );
}
