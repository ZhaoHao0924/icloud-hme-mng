import { Cloud, KeyRound, LogOut, Menu, Settings, Users, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { NavLink, useLocation, useNavigation, useOutlet } from "react-router-dom";

import { LoadingState } from "../components/LoadingState";
import { useApiToken } from "./apiTokenContext";
import { usePlatformAuth } from "./platformAuthContext";

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
  if (pathname.endsWith("/automation")) {
    return "自动化";
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
  const [mobileMenuPath, setMobileMenuPath] = useState<string | null>(null);
  const mobileMenuOpen = mobileMenuPath === pathname;
  const sidebarRef = useRef<HTMLElement>(null);
  const { clearApiToken, hasApiToken, openApiTokenDialog } = useApiToken();
  const { isLoggingOut, logout, status } = usePlatformAuth();

  useEffect(() => {
    if (!mobileMenuOpen) return;

    function handlePointerDown(event: PointerEvent) {
      if (!sidebarRef.current?.contains(event.target as Node)) {
        setMobileMenuPath(null);
      }
    }

    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        setMobileMenuPath(null);
      }
    }

    document.addEventListener("pointerdown", handlePointerDown);
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.removeEventListener("pointerdown", handlePointerDown);
      document.removeEventListener("keydown", handleKeyDown);
    };
  }, [mobileMenuOpen]);

  async function handleLogout() {
    clearApiToken();
    await logout();
  }

  return (
    <div className="app-shell">
      <aside className="sidebar" ref={sidebarRef}>
        <div className="brand">
          <span className="brand-mark" aria-hidden="true">
            <Cloud size={20} strokeWidth={2} />
          </span>
          <span>iCloud HME</span>
        </div>

        <button
          aria-controls="primary-navigation"
          aria-expanded={mobileMenuOpen}
          aria-label={mobileMenuOpen ? "收起主菜单" : "展开主菜单"}
          className="icon-button mobile-menu-button"
          type="button"
          onClick={() => setMobileMenuPath((current) => (current === pathname ? null : pathname))}
        >
          {mobileMenuOpen ? (
            <X size={19} aria-hidden="true" />
          ) : (
            <Menu size={19} aria-hidden="true" />
          )}
        </button>

        <nav
          className={`primary-nav${mobileMenuOpen ? " primary-nav-open" : ""}`}
          id="primary-navigation"
          aria-label="主导航"
        >
          {navigation.map(({ to, label, icon: Icon }) => (
            <NavLink
              className={({ isActive }) => `nav-link${isActive ? " nav-link-active" : ""}`}
              end={to === "/settings"}
              key={to}
              to={to}
              onClick={() => setMobileMenuPath(null)}
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
            {status?.username ? <span className="topbar-user">{status.username}</span> : null}
            <button
              className={`icon-button topbar-token-button${hasApiToken ? " topbar-token-configured" : ""}`}
              type="button"
              aria-label={hasApiToken ? "更新 API 访问令牌" : "输入 API 访问令牌"}
              title={hasApiToken ? "更新 API 访问令牌" : "输入 API 访问令牌"}
              onClick={openApiTokenDialog}
            >
              <KeyRound size={17} aria-hidden="true" />
            </button>
            <button
              className="icon-button"
              type="button"
              aria-label="退出登录"
              title="退出登录"
              disabled={isLoggingOut}
              onClick={() => void handleLogout()}
            >
              <LogOut size={17} aria-hidden="true" />
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
