"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useMemo, useState } from "react";
import { ChevronRightIcon, FolderIcon } from "lucide-react";

import { spaceLabel, useCloud } from "@/components/cloud/cloud-provider";
import { CountryFlag } from "@/components/country-flag";
import { OSIcon } from "@/components/os-icon";
import { ProtocolIcon } from "@/components/protocol-icon";
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuSub,
  ContextMenuSubContent,
  ContextMenuSubTrigger,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import type { ConnectionView, Script } from "@/lib/api";
import { cn } from "@/lib/utils";

export interface ConnectionActions {
  connect: (c: ConnectionView) => void;
  openFiles: (c: ConnectionView) => void;
  edit: (c: ConnectionView) => void;
  duplicate: (c: ConnectionView) => void;
  remove: (c: ConnectionView) => void;
  /** 复制到另一个空间 (换新 id, 跳板链一并复制)。 */
  copyTo: (c: ConnectionView, space: string, label: string) => void;
  /** 把整个文件夹复制到另一个空间 (文件夹名与组内跳板关系保留, 凭据与组外跳板机一并带过去)。 */
  copyGroupTo: (group: string, space: string, label: string) => void;
  /** 把这些连接 (连同跳板链) 的 Vaultwarden 凭据转为保存的凭据。 */
  convertVault: (ids: string[]) => void;
  runScript: (c: ConnectionView, s: Script) => void;
  manageScripts: () => void;
}

/** 侧栏连接列表: 按分组折叠, 双击连接, 右键更多操作。 */
export function ConnectionList({
  items,
  readOnly,
  scripts,
  query,
  selectedId,
  onSelect,
  actions,
}: {
  items: ConnectionView[];
  readOnly: boolean;
  scripts: Script[];
  query: string;
  selectedId: string;
  onSelect: (id: string) => void;
  actions: ConnectionActions;
}) {
  useLanguage();
  const { spaces } = useCloud();
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const targets = (spaces?.spaces ?? []).filter((s) => s.key !== spaces?.current && !s.readOnly);

  const groups = useMemo(() => {
    const q = query.trim().toLowerCase();
    const hit = q
      ? items.filter((c) => [c.name, c.host, c.username, c.group].some((v) => v.toLowerCase().includes(q)))
      : items;
    const map = new Map<string, ConnectionView[]>();
    for (const c of hit) {
      const list = map.get(c.group) ?? [];
      list.push(c);
      map.set(c.group, list);
    }
    // 未分组放最前, 其余按名称 (后端已排好序)。
    return [...map.entries()];
  }, [items, query]);

  if (items.length === 0) {
    return (
      <p className="text-muted-foreground px-4 py-6 text-center text-sm">
        {readOnly ? t("这个空间还没有连接") : t("还没有连接, 点上方 + 新建")}
      </p>
    );
  }
  if (groups.length === 0) {
    return <p className="text-muted-foreground px-4 py-6 text-center text-sm">{t("没有匹配的连接")}</p>;
  }

  return (
    <div className="space-y-1 py-1">
      {groups.map(([group, list]) => {
        const isCollapsed = !!collapsed[group] && !query;
        return (
          <div key={group || "__none"}>
            {group && (
              <ContextMenu>
                <ContextMenuTrigger asChild>
                  <button
                    type="button"
                    className="text-muted-foreground hover:text-foreground flex w-full items-center gap-1 px-3 py-1 text-xs font-medium"
                    onClick={() => setCollapsed((s) => ({ ...s, [group]: !s[group] }))}
                  >
                    <ChevronRightIcon className={cn("size-3 transition-transform", !isCollapsed && "rotate-90")} />
                    <FolderIcon className="size-3" />
                    <span className="truncate">{group}</span>
                    <span className="ml-auto tabular-nums">{list.length}</span>
                  </button>
                </ContextMenuTrigger>
                <ContextMenuContent className="w-52">
                  <ContextMenuItem onSelect={() => setCollapsed((s) => ({ ...s, [group]: !s[group] }))}>
                    {isCollapsed ? t("展开") : t("折叠")}
                  </ContextMenuItem>
                  {!readOnly && list.some((c) => c.auth === "vault") && (
                    <ContextMenuItem onSelect={() => actions.convertVault(list.map((c) => c.id))}>{t("Vaultwarden 凭据转为保存的凭据")}</ContextMenuItem>
                  )}
                  {targets.length > 0 && (
                    <ContextMenuSub>
                      <ContextMenuSubTrigger>{t("整个文件夹复制到…")}</ContextMenuSubTrigger>
                      <ContextMenuSubContent>
                        {targets.map((s) => (
                          <ContextMenuItem key={s.key} onSelect={() => actions.copyGroupTo(group, s.key, spaceLabel(s))}>
                            {spaceLabel(s)}
                          </ContextMenuItem>
                        ))}
                      </ContextMenuSubContent>
                    </ContextMenuSub>
                  )}
                </ContextMenuContent>
              </ContextMenu>
            )}
            {!isCollapsed &&
              list.map((c) => (
                <ConnectionRow
                  key={c.id}
                  c={c}
                  scripts={scripts}
                  indent={!!group}
                  selected={c.id === selectedId}
                  readOnly={readOnly}
                  onSelect={() => onSelect(c.id)}
                  actions={actions}
                />
              ))}
          </div>
        );
      })}
    </div>
  );
}

function ConnectionRow({
  c,
  scripts,
  indent,
  selected,
  readOnly,
  onSelect,
  actions,
}: {
  c: ConnectionView;
  scripts: Script[];
  indent: boolean;
  selected: boolean;
  readOnly: boolean;
  onSelect: () => void;
  actions: ConnectionActions;
}) {
  useLanguage();
  const { spaces } = useCloud();
  // 可复制去的空间: 除当前外、可写的
  const targets = (spaces?.spaces ?? []).filter((s) => s.key !== spaces?.current && !s.readOnly);
  const target = c.username ? `${c.username}@${c.host}` : c.host;
  return (
    <ContextMenu>
      <ContextMenuTrigger asChild>
        <button
          type="button"
          onClick={onSelect}
          onDoubleClick={() => actions.connect(c)}
          onContextMenu={onSelect}
          className={cn(
            "mx-1 flex w-[calc(100%-0.5rem)] items-center gap-2 rounded-md px-2 py-1.5 text-left",
            indent && "pl-5",
            selected ? "bg-brand-soft text-brand-strong" : "hover:bg-sidebar-accent/60",
          )}
        >
          {c.osId ? (
            <OSIcon osId={c.osId} title={c.osName} />
          ) : (
            <ProtocolIcon protocol={c.protocol} className="text-muted-foreground" />
          )}
          <span className="min-w-0 flex-1">
            <span className="block truncate text-sm">{c.name}</span>
            <span className="text-muted-foreground block truncate text-xs">{target}</span>
          </span>
          {c.countryCode && <CountryFlag code={c.countryCode} title={c.country || c.countryCode} />}
        </button>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={() => actions.connect(c)}>{t("连接")}</ContextMenuItem>
        {c.protocol === "ssh" && <ContextMenuItem onSelect={() => actions.openFiles(c)}>{t("文件")}</ContextMenuItem>}
        {c.protocol === "ssh" && (
          <ContextMenuSub>
            <ContextMenuSubTrigger>{t("运行脚本")}</ContextMenuSubTrigger>
            <ContextMenuSubContent className="czl-scroll max-h-80 overflow-y-auto">
              {scripts.map((s) => (
                <ContextMenuItem key={s.id} onSelect={() => actions.runScript(c, s)}>
                  {s.group ? `${s.group} / ${s.name}` : s.name}
                </ContextMenuItem>
              ))}
              {scripts.length > 0 && <ContextMenuSeparator />}
              <ContextMenuItem onSelect={actions.manageScripts}>{t("管理脚本…")}</ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => actions.edit(c)} disabled={readOnly}>{t("编辑")}</ContextMenuItem>
        <ContextMenuItem onSelect={() => actions.duplicate(c)} disabled={readOnly}>{t("复制")}</ContextMenuItem>
        {c.auth === "vault" && !readOnly && (
          <ContextMenuItem onSelect={() => actions.convertVault([c.id])}>{t("转为保存的凭据")}</ContextMenuItem>
        )}
        {targets.length > 0 && (
          <ContextMenuSub>
            <ContextMenuSubTrigger>{t("复制到…")}</ContextMenuSubTrigger>
            <ContextMenuSubContent>
              {targets.map((s) => (
                <ContextMenuItem key={s.key} onSelect={() => actions.copyTo(c, s.key, spaceLabel(s))}>
                  {spaceLabel(s)}
                </ContextMenuItem>
              ))}
            </ContextMenuSubContent>
          </ContextMenuSub>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem className="text-destructive focus:text-destructive" onSelect={() => actions.remove(c)} disabled={readOnly}>
          {t("删除")}</ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
