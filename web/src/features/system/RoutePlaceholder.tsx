type RoutePlaceholderProps = {
  title: string;
};

export function RoutePlaceholder({ title }: RoutePlaceholderProps) {
  return (
    <section className="status-page" aria-labelledby="route-placeholder-title">
      <div className="status-page-content">
        <h2 id="route-placeholder-title">{title}</h2>
        <p>暂无数据</p>
      </div>
    </section>
  );
}
