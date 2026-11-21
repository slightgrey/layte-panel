import { MutationCache, QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { BrowserRouter } from "react-router";
import { ApiError } from "./api/client";
import { meKey } from "./api/auth";
import { App } from "./App";
import "./index.css";

// When any request comes back 401 (session expired), re-check /auth/me;
// RequireAuth then redirects to the login page. Never for /auth/me itself:
// its 401 just means "not logged in", and re-checking it would loop forever.
const recheckSession = (err: unknown) => {
  if (err instanceof ApiError && err.status === 401) {
    queryClient.invalidateQueries({ queryKey: meKey });
  }
};
const isMeQuery = (key: readonly unknown[]) => key.length === meKey.length && meKey.every((k, i) => key[i] === k);

const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: (err, query) => !isMeQuery(query.queryKey) && recheckSession(err) }),
  mutationCache: new MutationCache({ onError: recheckSession }),
  defaultOptions: {
    queries: {
      refetchOnWindowFocus: true,
      retry: (count, err) => !(err instanceof ApiError && err.status < 500) && count < 2,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
);
