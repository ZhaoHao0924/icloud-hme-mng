import { Cloud, KeyRound, Settings, Users } from "lucide-react";
import { NavLink, useLocation, useNavigation, useOutlet } from "react-router-dom";

import { LoadingState } from "../components/LoadingState";
import { useApiToken } from "./apiTokenContext";

const navigation = [
  { to: "/accounts", label: "账户", icon: Users },
  { to: "/settings", label: "设置", icon: Settings },
];

function pageTitle(pathname: string) {
  if (pathname === "/settings") {
    return "系统设置";
  }
  if (pathname.endsWith("/aliases")) {
    return "别名";
  }
  if (pathname.endsWith("/inbox")) {
    return "收件箱";
  }
  if (pathname.endsWith("/security")) {
    return "凭据";
  }
  return "账户";
}

export function App() {
  const { pathname } = useLocation();
  const navigationState = useNavigation().state;
  const outlet = useOutlet();
  const isNavigating = navigationState !== "idle";
  const { hasApiToken, openApiTokenDialog } = useApiToken();

  return (
    <div className="app-shell">
      <aside className="sidebar">
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            <Cloud size={20} strokeWidth={2} />
          </span>
          <span>iCloud HME</span>
        </div>

        <nav className="primary-nav" aria-label="主导航">
          {navigation.map(({ to, label, icon: Icon }) => (
            <NavLink
              className={({ isActive }) => `nav-link${isActive ? " nav-link-active" : ""}`}
              end={to === "/settings"}
              key={to}
              to={to}
            >
              <Icon size={18} aria-hidden="true" />
              <span>{label}</span>
            </NavLink>
          ))}
        </nav>
      </aside>

      <div className="workspace">
        <header className="topbar">
          <h1>{pageTitle(pathname)}</h1>
          <div className="topbar-actions">
            <button
              className={`icon-button topbar-token-button${hasApiToken ? " topbar-token-configured" : ""}`}
              type="button"
              aria-label={hasApiToken ? "更新 API 访问令牌" : "输入 API 访问令牌"}
              title={hasApiToken ? "更新 API 访问令牌" : "输入 API 访问令牌"}
              onClick={openApiTokenDialog}
            >
              <KeyRound size={17} aria-hidden="true" />
            </button>
          </div>
        </header>

        <main aria-busy={isNavigating || undefined} className="content">
          {outlet ?? (isNavigating ? <LoadingState label="正在加载页面" /> : null)}
        </main>
      </div>
    </div>
  );
}
