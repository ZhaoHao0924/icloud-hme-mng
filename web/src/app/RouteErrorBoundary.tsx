import { RefreshCw } from "lucide-react";
import { useEffect, useRef } from "react";
import { isRouteErrorResponse, Link, useRouteError } from "react-router-dom";

function errorTitle(error: unknown) {
  if (isRouteErrorResponse(error)) {
    return `请求失败（${error.status}）`;
  }
  return "页面加载失败";
}

export function RouteErrorBoundary() {
  const error = useRouteError();
  const titleRef = useRef<HTMLHeadingElement>(null);

  useEffect(() => {
    titleRef.current?.focus();
  }, []);

  return (
    <main className="status-page" aria-labelledby="route-error-title" role="alert">
      <div className="status-page-content">
        <p className="status-code">!</p>
        <h1 id="route-error-title" ref={titleRef} tabIndex={-1}>
          {errorTitle(error)}
        </h1>
        <p>请重新加载页面，或返回账户页继续操作。</p>
        <div className="status-actions">
          <button
            className="button button-secondary"
            type="button"
            onClick={() => window.location.reload()}
          >
            <RefreshCw size={16} aria-hidden="true" />
            重新加载页面
          </button>
          <Link className="status-link" to="/accounts">
            返回账户
          </Link>
        </div>
      </div>
    </main>
  );
}
