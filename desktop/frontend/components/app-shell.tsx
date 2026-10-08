"use client";

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { ArrowUpCircleIcon, PlusIcon, SearchIcon } from "lucide-react";
import { toast } from "sonner";

import { ConnectionDetail, type DetailTab } from "@/components/connection-detail";
import { ConnectionForm } from "@/components/connection-form";
import { ConnectionList, type ConnectionActions } from "@/components/connection-list";
import { SettingsPage, type SettingsTab } from "@/components/settings-page";
import { StatusBar } from "@/components/status-bar";
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
  type SyncStatus,
  type UpdateInfo,
} from "@/lib/api";
import { errorText } from "@/lib/errors";

export function AppShell() {
  return (
    <VaultProvider>
      <Shell />
    </VaultProvider>
  );
}

function Shell() {
  const { withVault, status: vaultStatus } = useVault();
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
  const [settingsTab, setSettingsTab] = useState<SettingsTab>("apps");
  const [update, setUpdate] = useState<UpdateInfo | null>(null);
  const [removing, setRemoving] = useState<ConnectionView | null>(null);
  const [sync, setSync] = useState<SyncStatus | null>(null);
  const [syncEnabled, setSyncEnabled] = useState(false);
  // 坏文件告警只在内容变化时提示一次, 不随每次刷新重复弹出。
  const lastWarnings = useRef("");
  // 手动同步自己弹结果, 同步状态事件里不再重复提示。
  const manualSync = useRef(false);
  // 保存同步设置后安排的那次同步, 成功时也要告诉用户。
  const announceSync = useRef(false);

  const reload = useCallback(async () => {
    try {
      const l = await api.ListConnections();
      setList(l);
      setLoadError("");
      const w = l.warnings.join("\n");
      if (w && w !== lastWarnings.current) l.warnings.forEach((x) => toast.warning(errorText(x)));
      lastWarnings.current = w;
    } catch (e) {
      setLoadError(errorText(e));
    }
  }, []);

  const loadSettings = useCallback(() => {
    api
      .GetSettings()
      .then((v) => setSyncEnabled(!!v.settings.gitRemote))
      .catch(() => {});
  }, []);

  useEffect(() => {
    queueMicrotask(() => {
      void reload();
      loadSettings();
    });
    api.GetSyncStatus().then(setSync).catch(() => {});
    const offSync = onEvent<SyncStatus>(Events.syncStatus, (s) => {
      setSync(s);
      if (s.running) return;
      // 同步可能拉来了别的设备上的改动。
      void reload();
      if (!manualSync.current) {
        // 后台同步 (自动同步、保存设置后) 失败一律提示, 不能只藏在按钮的悬停说明里。
        if (s.error) toast.error(`同步失败: ${errorText(s.error)}`);
        else if (announceSync.current) toast.success("仓库同步完成");
      }
      announceSync.current = false;
    });
    api
      .GetPendingUpdate()
      .then((u) => u?.available && setUpdate(u))
      .catch(() => {});
    const offUpdate = onEvent<UpdateInfo>(Events.updateAvailable, setUpdate);
    const offEdit = onEvent<EditStatus>(Events.editStatus, (s) => {
      if (s.error) toast.error(`${s.remote} 上传失败: ${errorText(s.error)}`);
      else toast.success(`已保存到服务器: ${s.remote}`);
    });
    return () => {
      offSync();
      offEdit();
      offUpdate();
    };
  }, [reload, loadSettings]);

  // 保险库状态变化后条目名称 (列表里的派生字段) 随之出现或消失。
  useEffect(() => {
    queueMicrotask(() => void reload());
  }, [vaultStatus?.status, reload]);

  const items = useMemo(() => list?.items ?? [], [list]);
  const selected = items.find((c) => c.id === selectedId) ?? null;

  const actions: ConnectionActions = useMemo(
    () => ({
      connect: async (c) => {
        try {
          const r = await withVault(() => api.Connect(c.id));
          if (r.passwordCopied) toast.success(`正在打开 ${c.name}, 密码已复制到剪贴板, ${r.clipboardSeconds} 秒后清除`);
          else toast.success(`正在打开 ${c.name}`);
        } catch (e) {
          if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
        }
      },
      openFiles: (c) => {
        setSelectedId(c.id);
        setTab("files");
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
    }),
    [withVault, reload],
  );

  function create(protocol: Protocol) {
    const c = emptyConnection(protocol);
    if (selected?.group) c.group = selected.group;
    setFormInitial(c);
    setFormKey((k) => k + 1);
    setFormOpen(true);
  }

  function openSettings(tab: SettingsTab) {
    setSettingsTab(tab);
    setSettingsOpen(true);
  }

  // 设置是整页视图, 覆盖侧栏与主区域; 返回后回到原来的连接。
  if (settingsOpen) {
    return (
      <SettingsPage
        initialTab={settingsTab}
        onClose={() => setSettingsOpen(false)}
        onSaved={(syncing) => {
          announceSync.current = syncing;
          loadSettings();
          void reload();
        }}
      />
    );
  }

  /** 侧栏同步按钮: 连接配置走 git, 保险库已解锁时顺带从服务器同步条目 (bw sync)。 */
  async function syncNow() {
    const vaultOpen = vaultStatus?.status === "unlocked";
    manualSync.current = true;
    const [git, vault] = await Promise.allSettled([api.SyncNow(), vaultOpen ? api.VaultRefresh() : Promise.resolve()]);
    // 同步结束的事件可能晚于返回值到达, 稍等再清标记, 避免同一次结果提示两遍。
    setTimeout(() => (manualSync.current = false), 500);
    if (git.status === "rejected") toast.error(errorText(git.reason));
    if (vault.status === "rejected") toast.error(`保险库同步失败: ${errorText(vault.reason)}`);
    if (git.status === "fulfilled" && vault.status === "fulfilled") {
      const what = syncEnabled ? "连接配置已同步" : "连接配置已提交到本地仓库";
      toast.success(vaultOpen ? `${what}, 保险库条目已更新` : what);
    }
    if (vaultOpen) void reload();
  }

  return (
    <div className="flex h-full">
      <aside className="bg-sidebar text-sidebar-foreground flex w-72 shrink-0 flex-col border-r">
        <div className="flex shrink-0 items-center gap-1 p-2">
          <div className="relative min-w-0 flex-1">
            <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
            <Input className="h-7 pl-7 text-sm" placeholder="搜索" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" size="icon-sm" aria-label="新建连接">
                <PlusIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end">
              <DropdownMenuItem onSelect={() => create("ssh")}>新建 SSH</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => create("rdp")}>新建 RDP</DropdownMenuItem>
              <DropdownMenuItem onSelect={() => create("vnc")}>新建 VNC</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="czl-scroll min-h-0 flex-1 overflow-y-auto">
          {loadError ? (
            <p className="text-destructive px-4 py-6 text-sm">{loadError}</p>
          ) : (
            <ConnectionList
              items={items}
              query={query}
              selectedId={selectedId}
              onSelect={(id) => {
                if (id !== selectedId) setTab("overview");
                setSelectedId(id);
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
            <span className="truncate">新版本 {update.release.version} 可用, 点击更新</span>
          </button>
        )}
        <StatusBar sync={sync} syncEnabled={syncEnabled} onSync={syncNow} onSettings={() => openSettings("apps")} />
      </aside>

      <main className="bg-background min-w-0 flex-1">
        {selected ? (
          <ConnectionDetail key={selected.id} c={selected} tab={tab} onTabChange={setTab} actions={actions} />
        ) : (
          <div className="text-muted-foreground flex h-full flex-col items-center justify-center gap-2 text-sm">
            <p>选择左侧的连接, 双击直接连接</p>
          </div>
        )}
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
      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除连接 {removing?.name}?</AlertDialogTitle>
            <AlertDialogDescription>只删除连接配置, 保险库里的密码与私钥不受影响。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
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
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
