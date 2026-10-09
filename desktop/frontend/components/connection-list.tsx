"use client";

import { useMemo, useState } from "react";
import { ChevronRightIcon, FolderIcon } from "lucide-react";

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
  runScript: (c: ConnectionView, s: Script) => void;
  manageScripts: () => void;
}

/** 侧栏连接列表: 按分组折叠, 双击连接, 右键更多操作。 */
export function ConnectionList({
  items,
  scripts,
  query,
  selectedId,
  onSelect,
  actions,
}: {
  items: ConnectionView[];
  scripts: Script[];
  query: string;
  selectedId: string;
  onSelect: (id: string) => void;
  actions: ConnectionActions;
}) {
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});

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
    return <p className="text-muted-foreground px-4 py-6 text-center text-sm">还没有连接, 点上方 + 新建</p>;
  }
  if (groups.length === 0) {
    return <p className="text-muted-foreground px-4 py-6 text-center text-sm">没有匹配的连接</p>;
  }

  return (
    <div className="space-y-1 py-1">
      {groups.map(([group, list]) => {
        const isCollapsed = !!collapsed[group] && !query;
        return (
          <div key={group || "__none"}>
            {group && (
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
            )}
            {!isCollapsed &&
              list.map((c) => (
                <ConnectionRow
                  key={c.id}
                  c={c}
                  scripts={scripts}
                  indent={!!group}
                  selected={c.id === selectedId}
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
  onSelect,
  actions,
}: {
  c: ConnectionView;
  scripts: Script[];
  indent: boolean;
  selected: boolean;
  onSelect: () => void;
  actions: ConnectionActions;
}) {
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
            selected ? "bg-sidebar-accent text-sidebar-accent-foreground" : "hover:bg-sidebar-accent/60",
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
        </button>
      </ContextMenuTrigger>
      <ContextMenuContent>
        <ContextMenuItem onSelect={() => actions.connect(c)}>连接</ContextMenuItem>
        {c.protocol === "ssh" && <ContextMenuItem onSelect={() => actions.openFiles(c)}>文件</ContextMenuItem>}
        {c.protocol === "ssh" && (
          <ContextMenuSub>
            <ContextMenuSubTrigger>运行脚本</ContextMenuSubTrigger>
            <ContextMenuSubContent className="czl-scroll max-h-80 overflow-y-auto">
              {scripts.map((s) => (
                <ContextMenuItem key={s.id} onSelect={() => actions.runScript(c, s)}>
                  {s.group ? `${s.group} / ${s.name}` : s.name}
                </ContextMenuItem>
              ))}
              {scripts.length > 0 && <ContextMenuSeparator />}
              <ContextMenuItem onSelect={actions.manageScripts}>管理脚本…</ContextMenuItem>
            </ContextMenuSubContent>
          </ContextMenuSub>
        )}
        <ContextMenuSeparator />
        <ContextMenuItem onSelect={() => actions.edit(c)}>编辑</ContextMenuItem>
        <ContextMenuItem onSelect={() => actions.duplicate(c)}>复制</ContextMenuItem>
        <ContextMenuSeparator />
        <ContextMenuItem className="text-destructive focus:text-destructive" onSelect={() => actions.remove(c)}>
          删除
        </ContextMenuItem>
      </ContextMenuContent>
    </ContextMenu>
  );
}
