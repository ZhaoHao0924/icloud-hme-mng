import { Navigate, Outlet, useLocation } from "react-router-dom";

import { getApiErrorMessage } from "../api/client";
import { LoadingState } from "../components/LoadingState";
import { usePlatformAuth } from "./platformAuthContext";

export function PlatformAuthGate() {
  const location = useLocation();
  const { error, isLoading, refresh, status } = usePlatformAuth();

  if (isLoading) {
    return (
      <main className="platform-auth-page">
        <LoadingState label="正在检查登录状态" />
      </main>
    );
  }

  if (error) {
    return (
      <main className="platform-auth-page">
        <section
          className="platform-auth-panel platform-auth-unavailable"
          aria-labelledby="platform-auth-error-title"
        >
          <h1 id="platform-auth-error-title">无法确认登录状态</h1>
          <p>{getApiErrorMessage(error)}</p>
          <button className="button button-primary" type="button" onClick={() => void refresh()}>
            重新检查
          </button>
        </section>
      </main>
    );
  }

  if (!status?.authenticated) {
    return (
      <Navigate replace state={{ from: `${location.pathname}${location.search}` }} to="/login" />
    );
  }

  return <Outlet />;
}
