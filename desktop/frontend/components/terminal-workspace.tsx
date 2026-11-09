"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { LayoutListIcon, XIcon } from "lucide-react";
import { toast } from "sonner";

import { useLanguage } from "@/components/language-provider";
import { TERMINAL_BACKGROUND, TerminalView } from "@/components/terminal-view";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { api, Events, onEvent, type TerminalInfo } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { t } from "@/lib/i18n";
import { cn } from "@/lib/utils";

/** 主区域显示的视图: 连接详情或某个终端会话。 */
export const DETAIL_VIEW = "detail";

export interface TerminalTab {
  id: string;
  connId: string;
  scriptId: string;
  title: string;
  exited: boolean;
  exitCode: number;
}

function fromInfo(i: TerminalInfo): TerminalTab {
  return { id: i.id, connId: i.connId, scriptId: i.scriptId, title: i.title, exited: i.exited, exitCode: i.exitCode };
}

/**
 * 内置终端标签的状态。会话归后端, 挂载时按 ListTerminals 恢复标签。
 * relaunch 按原来的连接 / 脚本重新打开, 返回新会话; 用户取消时返回 null。
 */
export function useTerminalTabs(relaunch: (tab: TerminalTab) => Promise<TerminalInfo | null>) {
  const [tabs, setTabs] = useState<TerminalTab[]>([]);
  const [view, setView] = useState(DETAIL_VIEW);
  // 最新的标签列表, 供异步回调判断标签是否还在。
  const tabsRef = useRef(tabs);
  useEffect(() => {
    tabsRef.current = tabs;
  });
  // 已正常退出并关闭的会话: 退出事件可能早于打开标签到达, 这些 ID 不再开标签。
  const ended = useRef(new Set<string>());

  useEffect(() => {
    api
      .ListTerminals()
      .then((list) =>
        setTabs((cur) => {
          // 与挂载后已打开的标签按 ID 合并, 不丢后端仍在运行的会话。
          const have = new Set(cur.map((x) => x.id));
          return [...(list ?? []).filter((x) => !have.has(x.id)).map(fromInfo), ...cur];
        }),
      )
      .catch(() => {});
    return onEvent<TerminalInfo>(Events.terminalExit, (info) => {
      // 正常退出 (输入 exit) 直接关标签; 异常退出留着, 让用户看到报错并可重新连接。
      if (info.exitCode === 0) {
        ended.current.add(info.id);
        void api.CloseTerminal(info.id).catch(() => {});
        setTabs((cur) => cur.filter((x) => x.id !== info.id));
        setView((v) => (v === info.id ? DETAIL_VIEW : v));
        return;
      }
      setTabs((cur) => cur.map((x) => (x.id === info.id ? { ...x, exited: true, exitCode: info.exitCode } : x)));
    });
  }, []);

  // 关掉当前标签后切到相邻的标签, 没有了就回到连接详情。
  useEffect(() => {
    if (view !== DETAIL_VIEW && !tabs.some((x) => x.id === view)) {
      queueMicrotask(() => setView(tabs.at(-1)?.id ?? DETAIL_VIEW));
    }
  }, [tabs, view]);

  const open = useCallback((info: TerminalInfo) => {
    if (ended.current.has(info.id)) return;
    setTabs((cur) => [...cur.filter((x) => x.id !== info.id), fromInfo(info)]);
    setView(info.id);
  }, []);

  const close = useCallback(async (id: string) => {
    try {
      await api.CloseTerminal(id);
    } catch {
      /* 会话已不存在时照样移除标签 */
    }
    setTabs((cur) => cur.filter((x) => x.id !== id));
  }, []);

  const rename = useCallback(async (id: string, title: string) => {
    const next = title.trim();
    if (!next) return;
    try {
      await api.RenameTerminal(id, next);
      setTabs((cur) => cur.map((x) => (x.id === id ? { ...x, title: next } : x)));
    } catch (e) {
      toast.error(errorText(e));
    }
  }, []);

  // 重新连接在原标签里换成新会话, 保留标签位置与改过的标题。
  const relaunchRef = useRef(relaunch);
  useEffect(() => {
    relaunchRef.current = relaunch;
  });
  const busy = useRef(new Set<string>());
  const reconnect = useCallback(async (tab: TerminalTab) => {
    if (busy.current.has(tab.id)) return;
    busy.current.add(tab.id);
    try {
      const info = await relaunchRef.current(tab);
      if (!info) return;
      // 等待期间 (如解锁保险库) 标签被关掉了: 新会话没有地方显示, 直接结束。
      if (!tabsRef.current.some((x) => x.id === tab.id)) {
        void api.CloseTerminal(info.id).catch(() => {});
        return;
      }
      if (info.title !== tab.title) await api.RenameTerminal(info.id, tab.title).catch(() => {});
      void api.CloseTerminal(tab.id).catch(() => {});
      setTabs((cur) => cur.map((x) => (x.id === tab.id ? { ...fromInfo(info), title: tab.title } : x)));
      setView((v) => (v === tab.id ? info.id : v));
    } finally {
      busy.current.delete(tab.id);
    }
  }, []);

  const markExited = useCallback((id: string, code: number) => {
    setTabs((cur) => cur.map((x) => (x.id === id ? { ...x, exited: true, exitCode: code } : x)));
  }, []);

  return { tabs, view, setView, open, close, rename, reconnect, markExited };
}

type Terminals = ReturnType<typeof useTerminalTabs>;

/** 主区域顶部的标签栏: 第一个是连接详情, 后面是终端。有终端时才显示。 */
export function TerminalTabBar({ terms, detailLabel }: { terms: Terminals; detailLabel: string }) {
  useLanguage();
  const [editing, setEditing] = useState("");
  const [closing, setClosing] = useState<TerminalTab | null>(null);
  if (terms.tabs.length === 0) return null;

  function requestClose(tab: TerminalTab) {
    if (tab.exited) void terms.close(tab.id);
    else setClosing(tab);
  }

  return (
    <div className="bg-sidebar czl-scroll flex h-9 shrink-0 items-stretch overflow-x-auto border-b">
      <TabButton active={terms.view === DETAIL_VIEW} onClick={() => terms.setView(DETAIL_VIEW)}>
        <LayoutListIcon className="size-3.5 shrink-0" />
        <span className="max-w-40 truncate">{detailLabel}</span>
      </TabButton>
      {terms.tabs.map((tab) => (
        <TabButton
          key={tab.id}
          active={terms.view === tab.id}
          onClick={() => terms.setView(tab.id)}
          onMiddleClick={() => requestClose(tab)}
          onDoubleClick={() => setEditing(tab.id)}
        >
          <span
            className={cn("size-1.5 shrink-0 rounded-full", tab.exited ? "bg-muted-foreground/50" : "bg-emerald-500")}
            title={tab.exited ? t("已断开") : t("已连接")}
          />
          {editing === tab.id ? (
            <input
              autoFocus
              defaultValue={tab.title}
              className="bg-background w-32 rounded-sm border px-1 text-xs outline-none"
              onClick={(e) => e.stopPropagation()}
              onFocus={(e) => e.currentTarget.select()}
              onBlur={(e) => {
                setEditing("");
                void terms.rename(tab.id, e.currentTarget.value);
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") e.currentTarget.blur();
                if (e.key === "Escape") setEditing("");
              }}
            />
          ) : (
            <span className="max-w-48 truncate" title={tab.title}>
              {tab.title}
            </span>
          )}
          <span
            role="button"
            tabIndex={-1}
            aria-label={t("关闭终端")}
            className="hover:bg-accent text-muted-foreground hover:text-foreground -mr-1 rounded-sm p-0.5"
            onClick={(e) => {
              e.stopPropagation();
              requestClose(tab);
            }}
          >
            <XIcon className="size-3" />
          </span>
        </TabButton>
      ))}

      <AlertDialog open={!!closing} onOpenChange={(o) => !o && setClosing(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("关闭终端 {0}?", closing?.title)}</AlertDialogTitle>
            <AlertDialogDescription>{t("连接还在运行, 关闭会断开它。")}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                if (closing) void terms.close(closing.id);
                setClosing(null);
              }}
            >
              {t("关闭")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function TabButton({
  active,
  onClick,
  onMiddleClick,
  onDoubleClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  onMiddleClick?: () => void;
  onDoubleClick?: () => void;
  children: React.ReactNode;
}) {
  return (
    <div
      role="tab"
      aria-selected={active}
      className={cn(
        "flex shrink-0 cursor-default items-center gap-1.5 border-r px-3 text-xs select-none",
        active ? "bg-background text-foreground" : "text-muted-foreground hover:text-foreground hover:bg-background/50",
      )}
      onClick={onClick}
      onDoubleClick={onDoubleClick}
      onAuxClick={(e) => {
        if (e.button === 1) onMiddleClick?.();
      }}
    >
      {children}
    </div>
  );
}

/** 全部终端会话的显示。切走的标签只隐藏不卸载, 内容与滚动位置都在。 */
export function TerminalPanes({ terms }: { terms: Terminals }) {
  return (
    <>
      {terms.tabs.map((tab) => (
        <div
          key={tab.id}
          className={cn("absolute inset-0", terms.view !== tab.id && "invisible")}
          style={{ background: TERMINAL_BACKGROUND }}
        >
          <TerminalView
            sessionId={tab.id}
            active={terms.view === tab.id}
            onExit={(code) => terms.markExited(tab.id, code)}
            onReconnect={() => void terms.reconnect(tab)}
          />
        </div>
      ))}
    </>
  );
}
