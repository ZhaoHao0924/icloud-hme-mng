import { useQuery } from "@tanstack/react-query";
import { AtSign, RotateCcw, Search, X } from "lucide-react";
import { useMemo } from "react";
import { useSearchParams } from "react-router-dom";

import { aliasesQueryOptions } from "../../api/queries";
import type { Alias } from "../../api/schemas";
import { EmptyState } from "../../components/EmptyState";
import { LoadingState } from "../../components/LoadingState";
import { useAccountDetailContext } from "../accounts/accountDetailContext";
import { AccountRequestErrorState } from "../security/SessionRecoveryView";
import { AliasCopyButton } from "./AliasCopyButton";
import { AliasDeleteButton } from "./AliasDeleteButton";
import { AliasStatusButton } from "./AliasStatusButton";
import { CreateAliasDialog } from "./CreateAliasDialog";

const statusFilters = [
  { label: "全部", value: "all" },
  { label: "使用中", value: "active" },
  { label: "已停用", value: "inactive" },
] as const;

type AliasStatusFilter = (typeof statusFilters)[number]["value"];

function parseStatusFilter(value: string | null): AliasStatusFilter {
  return value === "active" || value === "inactive" ? value : "all";
}

function formatAliasDate(value: string) {
  if (!value) return "未知";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知";
  return new Intl.DateTimeFormat("zh-CN", {
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    month: "2-digit",
    year: "numeric",
  }).format(date);
}

function AliasCreatedAt({ value }: { value: string }) {
  const label = formatAliasDate(value);
  return value && label !== "未知" ? <time dateTime={value}>{label}</time> : <span>{label}</span>;
}

export function AliasesPage() {
  const { account } = useAccountDetailContext();
  const [searchParams, setSearchParams] = useSearchParams();
  const aliasesQuery = useQuery(aliasesQueryOptions(account.id));
  const searchQuery = searchParams.get("q") ?? "";
  const statusFilter = parseStatusFilter(searchParams.get("status"));
  const aliases = useMemo(() => aliasesQuery.data?.aliases ?? [], [aliasesQuery.data]);

  const statusCounts = useMemo(
    () => ({
      active: aliases.filter((alias) => alias.active).length,
      all: aliases.length,
      inactive: aliases.filter((alias) => !alias.active).length,
    }),
    [aliases],
  );

  const visibleAliases = useMemo(() => {
    const normalizedQuery = searchQuery.trim().toLocaleLowerCase();
    return aliases.filter((alias) => {
      const matchesStatus =
        statusFilter === "all" || (statusFilter === "active" ? alias.active : !alias.active);
      if (!matchesStatus) return false;
      if (!normalizedQuery) return true;
      return [alias.email, alias.label, alias.anonymousId].some((value) =>
        value.toLocaleLowerCase().includes(normalizedQuery),
      );
    });
  }, [aliases, searchQuery, statusFilter]);

  function updateFilters(nextQuery: string, nextStatus: AliasStatusFilter) {
    const nextParams = new URLSearchParams(searchParams);
    if (nextQuery) {
      nextParams.set("q", nextQuery);
    } else {
      nextParams.delete("q");
    }
    if (nextStatus === "all") {
      nextParams.delete("status");
    } else {
      nextParams.set("status", nextStatus);
    }
    setSearchParams(nextParams, { replace: true });
  }

  const hasFilters = searchQuery !== "" || statusFilter !== "all";
  const countLabel = hasFilters
    ? `${visibleAliases.length} / ${aliases.length} 个别名`
    : `${aliases.length} 个别名`;

  return (
    <section className="alias-page" aria-labelledby="alias-list-title">
      <div className="section-heading alias-section-heading">
        <div>
          <h3 id="alias-list-title">邮箱别名</h3>
          <span className="record-count">{aliasesQuery.isSuccess ? countLabel : "正在同步"}</span>
        </div>
        {aliasesQuery.isSuccess && aliases.length > 0 ? (
          <CreateAliasDialog
            accountId={account.id}
            accountName={account.name}
            onCreated={() => updateFilters("", "all")}
          />
        ) : null}
      </div>

      {aliasesQuery.isPending ? (
        <div className="table-frame alias-table-state">
          <LoadingState label="正在读取别名" />
        </div>
      ) : null}

      {aliasesQuery.isError ? (
        <AccountRequestErrorState
          accountId={account.id}
          error={aliasesQuery.error}
          onRetry={() => void aliasesQuery.refetch()}
          title="别名加载失败"
        />
      ) : null}

      {aliasesQuery.isSuccess && aliases.length === 0 ? (
        <div className="table-frame alias-table-state">
          <EmptyState
            action={
              <CreateAliasDialog
                accountId={account.id}
                accountName={account.name}
                onCreated={() => updateFilters("", "all")}
              />
            }
            description="此账户当前没有 Hide My Email 别名。"
            icon={<AtSign size={22} />}
            title="暂无别名"
          />
        </div>
      ) : null}

      {aliasesQuery.isSuccess && aliases.length > 0 ? (
        <>
          <div className="alias-toolbar">
            <div className="alias-search-control">
              <Search size={17} aria-hidden="true" />
              <label className="visually-hidden" htmlFor="alias-search">
                搜索别名
              </label>
              <input
                id="alias-search"
                autoCapitalize="none"
                autoComplete="off"
                maxLength={320}
                placeholder="搜索邮箱、标签或 ID"
                type="search"
                value={searchQuery}
                onChange={(event) => updateFilters(event.target.value, statusFilter)}
              />
              {searchQuery ? (
                <button
                  className="icon-button alias-search-clear"
                  type="button"
                  aria-label="清除搜索"
                  title="清除搜索"
                  onClick={() => updateFilters("", statusFilter)}
                >
                  <X size={15} aria-hidden="true" />
                </button>
              ) : null}
            </div>

            <div className="segmented-control" role="group" aria-label="别名状态筛选">
              {statusFilters.map((filter) => (
                <button
                  className={`segmented-option${statusFilter === filter.value ? " segmented-option-active" : ""}`}
                  type="button"
                  aria-pressed={statusFilter === filter.value}
                  key={filter.value}
                  onClick={() => updateFilters(searchQuery, filter.value)}
                >
                  <span>{filter.label}</span>
                  <span className="segmented-count" aria-hidden="true">
                    {statusCounts[filter.value]}
                  </span>
                </button>
              ))}
            </div>
          </div>

          {visibleAliases.length === 0 ? (
            <div className="table-frame alias-table-state">
              <EmptyState
                action={
                  <button
                    className="button button-secondary"
                    type="button"
                    onClick={() => updateFilters("", "all")}
                  >
                    <RotateCcw size={16} aria-hidden="true" />
                    清除筛选
                  </button>
                }
                icon={<Search size={22} />}
                title="没有匹配的别名"
              />
            </div>
          ) : (
            <div className="table-frame">
              <table className="alias-table">
                <caption className="visually-hidden">别名列表</caption>
                <thead>
                  <tr>
                    <th scope="col">邮箱</th>
                    <th scope="col">标签</th>
                    <th scope="col">状态</th>
                    <th scope="col">创建时间</th>
                    <th scope="col">操作</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleAliases.map((alias: Alias) => (
                    <tr key={alias.anonymousId}>
                      <th scope="row" data-label="邮箱">
                        <span className="alias-email-row">
                          <span className="alias-email">{alias.email}</span>
                          <AliasCopyButton email={alias.email} />
                        </span>
                        <span className="alias-id">{alias.anonymousId}</span>
                      </th>
                      <td data-label="标签">
                        <span className={alias.label ? "alias-label" : "alias-label-empty"}>
                          {alias.label || "未设置标签"}
                        </span>
                      </td>
                      <td data-label="状态">
                        <span
                          className={`status-chip status-chip-${alias.active ? "success" : "warning"}`}
                        >
                          <span className="status-chip-dot" aria-hidden="true" />
                          {alias.active ? "使用中" : "已停用"}
                        </span>
                      </td>
                      <td data-label="创建时间">
                        <AliasCreatedAt value={alias.createdAt} />
                      </td>
                      <td data-label="操作">
                        <div className="alias-action-group">
                          <AliasStatusButton accountId={account.id} alias={alias} />
                          <AliasDeleteButton accountId={account.id} alias={alias} />
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      ) : null}
    </section>
  );
}
