"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowUpCircleIcon, PlusIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";

import { CloudProvider, useCloud } from "@/components/cloud/cloud-provider";
import { ConfirmCancelled, ConfirmConnectionProvider, useConfirmGate } from "@/components/cloud/confirm-connection";
import { SpaceSwitcher } from "@/components/cloud/space-switcher";
import { ConnectionDetail, type DetailTab } from "@/components/connection-detail";
import { ConnectionForm } from "@/components/connection-form";
import { ConnectionList, type ConnectionActions } from "@/components/connection-list";
import { HostKeyDialog } from "@/components/host-key-dialog";
import { ScriptsPage } from "@/components/scripts-page";
import { SettingsPage, type SettingsTab } from "@/components/settings-page";
import { StatusBar } from "@/components/status-bar";
import { DETAIL_VIEW, TerminalPanes, TerminalTabBar, useTerminalTabs, type TerminalTab } from "@/components/terminal-workspace";
import { UnlockCancelled, useVault, VaultProvider } from "@/components/vault-provider";
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
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { Input } from "@/components/ui/input";
import {
  api,
  emptyConnection,
  Events,
  onEvent,
  type Connection,
  type ConnectionList as List,
  type ConnectionView,
  type EditStatus,
  type Protocol,
  type Script,
  type ScriptList,
  type TerminalInfo,
  type UpdateInfo,
} from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

export function AppShell() {
  return (
    <VaultProvider>
      <CloudProvider>
        <ConfirmConnectionProvider>
          <Shell />
        </ConfirmConnectionProvider>
      </CloudProvider>
    </VaultProvider>
  );
}

function Shell() {
  useLanguage();
  const { withVault, status: vaultStatus } = useVault();
  const { current } = useCloud();
  // 连接前可能要先解锁保险库, 团队连接可能要先确认 (6416); 两层都包上
  const confirmGate = useConfirmGate();
  const guarded = useCallback(<T,>(fn: () => Promise<T>) => confirmGate(() => withVault(fn)), [confirmGate, withVault]);
  const [list, setList] = useState<List | null>(null);
  const [loadError, setLoadError] = useState("");
  const [query, setQuery] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [tab, setTab] = useState<DetailTab>("overview");
  const [formInitial, setFormInitial] = useState<ConnectionView | Connection | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  // 每次打开表单换一个 key, 让表单按新的初始值重新挂载。
  const [formKey, setFormKey] = useState(0);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("general");
  const [scripts, setScripts] = useState<ScriptList | null>(null);
  const [scriptsOpen, setScriptsOpen] = useState(false);
  const [update, setUpdate] = useState<UpdateInfo | null>(null);
  const [removing, setRemoving] = useState<ConnectionView | null>(null);
  // 坏文件告警只在内容变化时提示一次, 不随每次刷新重复弹出。
  const lastWarnings = useRef("");

  const reload = useCallback(async () => {
    try {
      const l = await api.ListConnections();
      setList(l);
      setLoadError("");
      const w = l.warnings.join("\n");
      if (w && w !== lastWarnings.current) l.warnings.forEach((x) => toast.warning(errorText(x)));
      lastWarnings.current = w;
    } catch (e) {
      setLoadError(e instanceof Error ? e.message : String(e));
    }
  }, []);

  const reloadScripts = useCallback(async () => {
    try {
      setScripts(await api.ListScripts());
    } catch (e) {
      toast.error(errorText(e));
    }
  }, []);

  useEffect(() => {
    queueMicrotask(() => {
      void reload();
      void reloadScripts();
    });
    // 切换空间: 列表整体换掉, 选中项清空; 云同步拉到别处的修改: 原地刷新。
    const offSpace = onEvent(Events.spaceChanged, () => {
      setSelectedId("");
      setQuery("");
      void reload();
      void reloadScripts();
    });
    const offCloud = onEvent(Events.cloudChanged, () => {
      void reload();
      void reloadScripts();
    });
    api
      .GetPendingUpdate()
      .then((u) => u?.available && setUpdate(u))
      .catch(() => {});
    const offUpdate = onEvent<UpdateInfo>(Events.updateAvailable, setUpdate);
    const offEdit = onEvent<EditStatus>(Events.editStatus, (s) => {
      if (s.error) toast.error(t("{0} 上传失败: {1}", s.remote, errorText(s.error)));
      else toast.success(t("已保存到服务器: {0}", s.remote));
    });
    // 后台查到服务器所在国家后重新拉列表显示国旗。
    const offGeo = onEvent(Events.geoUpdated, () => void reload());
    return () => {
      offSpace();
      offCloud();
      offEdit();
      offUpdate();
      offGeo();
    };
  }, [reload, reloadScripts]);

  // 生效的当前空间变了 (启动后云同步就绪、换账号、空间被清掉回落到本机): 列表跟着换。
  useEffect(() => {
    queueMicrotask(() => {
      setSelectedId("");
      void reload();
      void reloadScripts();
    });
  }, [current.key, reload, reloadScripts]);

  // 保险库状态变化后条目名称 (列表里的派生字段) 随之出现或消失。
  useEffect(() => {
    queueMicrotask(() => void reload());
  }, [vaultStatus?.status, reload]);

  const items = useMemo(() => list?.items ?? [], [list]);
  const selected = items.find((c) => c.id === selectedId) ?? null;

  const scriptItems = useMemo(() => scripts?.items ?? [], [scripts]);

  // 内置终端: 打开连接 / 运行脚本返回会话 ID 时在主区域新开标签; 走外部终端时返回空串。
  const relaunch = useCallback(
    async (tab: TerminalTab): Promise<TerminalInfo | null> => {
      try {
        const id = tab.scriptId
          ? await guarded(() => api.RunScript(tab.connId, tab.scriptId))
          : (await guarded(() => api.Connect(tab.connId))).terminalId;
        return id ? await terminalInfo(id) : null;
      } catch (e) {
        if (!(e instanceof UnlockCancelled) && !(e instanceof ConfirmCancelled)) toast.error(errorText(e));
        return null;
      }
    },
    [guarded],
  );
  const terms = useTerminalTabs(relaunch);
  const openTerminal = terms.open;
  const openVNC = terms.openVNC;

  // 运行脚本与连接一样可能要先解锁保险库。
  const runScript = useCallback(
    async (c: ConnectionView, s: Script) => {
      try {
        const id = await guarded(() => api.RunScript(c.id, s.id));
        const info = id ? await terminalInfo(id) : null;
        if (info) {
          openTerminal(info);
          setScriptsOpen(false);
        } else if (!id) toast.success(t("正在 {0} 上运行 {1}", c.name, s.name));
      } catch (e) {
        if (!(e instanceof UnlockCancelled) && !(e instanceof ConfirmCancelled)) toast.error(errorText(e));
      }
    },
    [guarded, openTerminal],
  );

  const showDetail = terms.setView;
  const actions: ConnectionActions = useMemo(
    () => ({
      connect: async (c) => {
        try {
          const r = await guarded(() => api.Connect(c.id));
          if (r.vncId) {
            openVNC({ id: r.vncId, connId: c.id, title: c.name });
            return;
          }
          const info = r.terminalId ? await terminalInfo(r.terminalId) : null;
          // 走内置终端但会话已经结束 (秒退) 时不再提示「正在打开」。
          if (info) openTerminal(info);
          else if (r.terminalId) return;
          else if (r.passwordCopied) toast.success(t("正在打开 {0}, 密码已复制到剪贴板, {1} 秒后清除", c.name, r.clipboardSeconds));
          else toast.success(t("正在打开 {0}", c.name));
        } catch (e) {
          if (!(e instanceof UnlockCancelled) && !(e instanceof ConfirmCancelled)) toast.error(errorText(e));
        }
      },
      openFiles: (c) => {
        setSelectedId(c.id);
        setTab("files");
        showDetail(DETAIL_VIEW);
      },
      edit: (c) => {
        setFormInitial(c);
        setFormKey((k) => k + 1);
        setFormOpen(true);
      },
      duplicate: async (c) => {
        try {
          const copy = await api.DuplicateConnection(c.id);
          await reload();
          setSelectedId(copy.id);
        } catch (e) {
          toast.error(errorText(e));
        }
      },
      remove: (c) => setRemoving(c),
      copyTo: async (c, key, label) => {
        try {
          await api.CopyConnectionTo(c.id, key);
          toast.success(t("已复制到「{0}」", label));
        } catch (e) {
          toast.error(errorText(e));
        }
      },
      copyGroupTo: async (group, key, label) => {
        try {
          const n = await api.CopyGroupTo(group, key);
          toast.success(t("已把文件夹「{0}」的 {1} 条连接复制到「{2}」", group, n, label));
        } catch (e) {
          toast.error(errorText(e));
        }
      },
      runScript,
      manageScripts: () => setScriptsOpen(true),
    }),
    [guarded, reload, runScript, openTerminal, openVNC, showDetail],
  );

  function create(protocol: Protocol) {
    const c = emptyConnection(protocol);
    if (selected?.group) c.group = selected.group;
    setFormInitial(c);
    setFormKey((k) => k + 1);
    setFormOpen(true);
  }

  const readOnly = !!list?.readOnly || current.readOnly;

  function openSettings(tab: SettingsTab) {
    setSettingsTab(tab);
    setSettingsOpen(true);
  }

  // 设置与脚本库是整页视图, 盖住侧栏与主区域; 主界面只隐藏不卸载, 内置终端的内容都还在。
  const page = scriptsOpen ? (
    <ScriptsPage
      scripts={scriptItems}
      groups={scripts?.groups ?? []}
      connections={items}
      initialConnId={selectedId}
      onClose={() => setScriptsOpen(false)}
      onChanged={reloadScripts}
      onRun={runScript}
    />
) : settingsOpen ? (
    <SettingsPage
      initialTab={settingsTab}
      onClose={() => setSettingsOpen(false)}
      onSaved={() => void reload()}
    />
  ) : null;

  return (
    <>
      {page}
      <div className={cn("flex h-full", page && "hidden")}>
        <aside className="bg-sidebar text-sidebar-foreground flex w-72 shrink-0 flex-col border-r">
          <div className="flex shrink-0 items-center gap-1 px-1.5 pt-1.5">
            <SpaceSwitcher onManage={openSettings} />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon-sm" aria-label={t("新建连接")} disabled={readOnly} title={readOnly ? t("这个空间只读") : undefined}>
                  <PlusIcon />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={() => create("ssh")}>{t("新建 SSH")}</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => create("rdp")}>{t("新建 RDP")}</DropdownMenuItem>
                <DropdownMenuItem onSelect={() => create("vnc")}>{t("新建 VNC")}</DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
          <div className="relative shrink-0 px-2 py-1.5">
            <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-4 size-3.5 -translate-y-1/2" />
            <Input className="h-7 pl-7 text-sm" placeholder={t("搜索")} value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <div className="czl-scroll min-h-0 flex-1 overflow-y-auto">
            {loadError ? (
              <p className="text-destructive px-4 py-6 text-sm">{errorText(loadError)}</p>
            ) : (
              <ConnectionList
                items={items}
                readOnly={readOnly}
                query={query}
                scripts={scriptItems}
                selectedId={selectedId}
                onSelect={(id) => {
                  if (id !== selectedId) setTab("overview");
                  setSelectedId(id);
                  terms.setView(DETAIL_VIEW);
                }}
                actions={actions}
              />
            )}
          </div>
          {update?.release && (
            <button
              type="button"
              className="bg-accent text-accent-foreground mx-2 mb-1 flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs"
              onClick={() => openSettings("about")}
            >
              <ArrowUpCircleIcon className="size-4 shrink-0" />
              <span className="truncate">{t("新版本 {0} 可用, 点击更新", update.release.version)}</span>
            </button>
          )}
          <StatusBar onScripts={() => setScriptsOpen(true)} onSettings={() => openSettings("general")} onAccount={openSettings} />
        </aside>

        <main className="bg-background flex min-w-0 flex-1 flex-col">
          <TerminalTabBar terms={terms} detailLabel={selected?.name ?? t("连接详情")} />
          <div className="relative min-h-0 flex-1">
            <div className={cn("h-full", terms.view !== DETAIL_VIEW && "hidden")}>
              {selected ? (
                <ConnectionDetail key={selected.id} c={selected} scripts={scriptItems} tab={tab} onTabChange={setTab} actions={actions} />
              ) : (
                <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2 text-sm">
                  <p>{t("选择左侧的连接, 双击直接连接")}</p>
                </div>
              )}
            </div>
            <TerminalPanes terms={terms} />
          </div>
        </main>

        <ConnectionForm
          key={formKey}
          open={formOpen}
          initial={formInitial}
          all={items}
          groups={list?.groups ?? []}
          onOpenChange={setFormOpen}
          onSaved={async (c) => {
            setFormOpen(false);
            await reload();
            setSelectedId(c.id);
          }}
        />
        <HostKeyDialog />

        <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
          <AlertDialogContent>
            <AlertDialogHeader>
              <AlertDialogTitle>{t("删除连接 {0}?", removing?.name)}</AlertDialogTitle>
              <AlertDialogDescription>{t("只删除连接配置, 保险库里的密码与私钥不受影响。")}</AlertDialogDescription>
            </AlertDialogHeader>
            <AlertDialogFooter>
              <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
              <AlertDialogAction
                onClick={async () => {
                  const c = removing;
                  setRemoving(null);
                  if (!c) return;
                  try {
                    await api.DeleteConnection(c.id);
                    if (selectedId === c.id) setSelectedId("");
                    await reload();
                  } catch (e) {
                    toast.error(errorText(e));
                  }
                }}
              >
                {t("删除")}
                </AlertDialogAction>
            </AlertDialogFooter>
          </AlertDialogContent>
        </AlertDialog>
      </div>
    </>
  );
}

/** 按会话 ID 取内置终端的描述, 用来新开标签。 */
async function terminalInfo(id: string): Promise<TerminalInfo | null> {
  const list = await api.ListTerminals();
  return list.find((x) => x.id === id) ?? null;
}
