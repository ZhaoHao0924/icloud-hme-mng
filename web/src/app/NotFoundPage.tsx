import { Link } from "react-router-dom";

export function NotFoundPage() {
  return (
    <section className="status-page" aria-labelledby="not-found-title">
      <div className="status-page-content">
        <p className="status-code">404</p>
        <h2 id="not-found-title">找不到页面</h2>
        <p>请求的页面不存在。</p>
        <Link className="status-link" to="/accounts">
          返回账户
        </Link>
      </div>
    </section>
  );
}
