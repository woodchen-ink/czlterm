"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  ArrowUpIcon,
  ClipboardPasteIcon,
  FileIcon,
  FolderIcon,
  FolderPlusIcon,
  Loader2Icon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react";
import { toast } from "sonner";

import { ChmodDialog, type ChmodRequest } from "@/components/chmod-dialog";
import { PromptDialog, type PromptRequest } from "@/components/prompt-dialog";
import { UnlockCancelled, useVault } from "@/components/vault-provider";
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
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuShortcut,
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
import { api, type Entry, type Listing } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes, formatTime, joinPath, parentPath } from "@/lib/format";
import { cn } from "@/lib/utils";

/** 复制 / 剪切后待粘贴的路径。只在当前连接内有效。 */
interface Clipboard {
  mode: "copy" | "cut";
  paths: string[];
}

const mod = typeof navigator !== "undefined" && /Mac/i.test(navigator.platform) ? "⌘" : "Ctrl+";

/**
 * SFTP 文件管理。双击目录进入, 双击文件用外部编辑器打开, 保存后自动传回。
 * 支持 Ctrl/Shift 多选, 右键与快捷键执行复制、剪切、粘贴、重命名、改权限与删除。
 */
export function FileBrowser({ connId }: { connId: string }) {
  useLanguage();
  const { withVault } = useVault();
  const [listing, setListing] = useState<Listing | null>(null);
  const [pathInput, setPathInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState<string[]>([]);
  const [anchor, setAnchor] = useState("");
  const [clipboard, setClipboard] = useState<Clipboard | null>(null);
  const [menuTarget, setMenuTarget] = useState<Entry | null>(null);
  const [prompt, setPrompt] = useState<PromptRequest | null>(null);
  const [chmod, setChmod] = useState<ChmodRequest | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<Entry[] | null>(null);

  const load = useCallback(
    async (dir: string) => {
      setLoading(true);
      setError("");
      try {
        const l = await withVault(() => api.FilesList(connId, dir));
        setListing(l);
        setPathInput(l.path);
        setSelected([]);
        setAnchor("");
      } catch (e) {
        if (!(e instanceof UnlockCancelled)) setError(e instanceof Error ? e.message : String(e));
      } finally {
        setLoading(false);
      }
    },
    [connId, withVault],
  );

  useEffect(() => {
    queueMicrotask(() => void load(""));
  }, [load]);

  const cwd = listing?.path ?? "";
  const entries = useMemo(() => listing?.entries ?? [], [listing]);
  const selectedSet = useMemo(() => new Set(selected), [selected]);
  const selectedEntries = useMemo(() => entries.filter((e) => selectedSet.has(e.path)), [entries, selectedSet]);
  const cutSet = useMemo(() => new Set(clipboard?.mode === "cut" ? clipboard.paths : []), [clipboard]);

  /** 执行一次文件操作并刷新当前目录; 批量操作可能部分成功, 失败时也刷新。 */
  async function run(fn: () => Promise<unknown>, success?: string) {
    try {
      await withVault(fn);
      if (success) toast.success(success);
    } catch (e) {
      if (e instanceof UnlockCancelled) return;
      toast.error(errorText(e));
    }
    await load(cwd);
  }

  async function edit(entry: Entry) {
    try {
      await withVault(() => api.FilesEdit(connId, entry.path));
      toast.info(t("已在编辑器中打开 {0}, 保存后自动上传", entry.name));
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    }
  }

  async function download(entry: Entry) {
    try {
      const saved = await withVault(() => api.FilesDownload(connId, entry.path));
      if (saved) toast.success(t("已下载到 {0}", saved));
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    }
  }

  function open(entry: Entry) {
    if (entry.isDir) void load(entry.path);
    else void edit(entry);
  }

  /** 单击选中; Ctrl/⌘ 增减, Shift 从锚点连选。 */
  function clickRow(entry: Entry, ev: React.MouseEvent) {
    if (ev.shiftKey && anchor) {
      const a = entries.findIndex((e) => e.path === anchor);
      const b = entries.findIndex((e) => e.path === entry.path);
      if (a >= 0 && b >= 0) {
        setSelected(entries.slice(Math.min(a, b), Math.max(a, b) + 1).map((e) => e.path));
        return;
      }
    }
    if (ev.ctrlKey || ev.metaKey) {
      setSelected((s) => (s.includes(entry.path) ? s.filter((p) => p !== entry.path) : [...s, entry.path]));
    } else {
      setSelected([entry.path]);
    }
    setAnchor(entry.path);
  }

  function toClipboard(mode: Clipboard["mode"], items: Entry[]) {
    if (items.length === 0) return;
    setClipboard({ mode, paths: items.map((e) => e.path) });
    toast.info(mode === "copy" ? t("已复制 {0} 项, 到目标目录粘贴", items.length) : t("已剪切 {0} 项, 到目标目录粘贴", items.length));
  }

  async function paste(dir: string) {
    if (!clipboard) return;
    const { mode, paths } = clipboard;
    const id = toast.loading(mode === "copy" ? t("正在复制…") : t("正在移动…"));
    try {
      const n = await withVault(() => (mode === "copy" ? api.FilesCopy(connId, paths, dir) : api.FilesMove(connId, paths, dir)));
      if (mode === "cut") setClipboard(null);
      toast.success(mode === "copy" ? t("已复制 {0} 项", n) : t("已移动 {0} 项", n), { id });
    } catch (e) {
      if (e instanceof UnlockCancelled) toast.dismiss(id);
      else toast.error(errorText(e), { id });
    }
    await load(cwd);
  }

  function rename(entry: Entry) {
    setPrompt({
      title: t("重命名"),
      initial: entry.name,
      confirm: t("重命名"),
      onSubmit: (name) => {
        if (name !== entry.name) return run(() => api.FilesRename(connId, entry.path, joinPath(cwd, name)));
      },
    });
  }

  function moveTo(items: Entry[]) {
    setPrompt({
      title: items.length === 1 ? t("移动 {0} 到", items[0].name) : t("移动 {0} 项到", items.length),
      initial: cwd,
      confirm: t("移动"),
      onSubmit: (dir) =>
        run(async () => {
          const n = await api.FilesMove(connId, items.map((e) => e.path), joinPath(cwd, dir));
          toast.success(t("已移动 {0} 项", n));
        }),
    });
  }

  function changeMode(items: Entry[]) {
    setChmod({
      entries: items,
      onSubmit: (mode, scope) => run(() => api.FilesChmod(connId, items.map((e) => e.path), mode, scope), t("权限已修改")),
    });
  }

  function copyPath(paths: string[]) {
    navigator.clipboard.writeText(paths.join("\n")).then(
      () => toast.success(t("已复制")),
      () => toast.error(t("复制失败")),
    );
  }

  function remove(items: Entry[]) {
    void run(async () => {
      for (const e of items) await api.FilesRemove(connId, e.path);
    }, items.length === 1 ? t("已删除 {0}", items[0].name) : t("已删除 {0} 项", items.length));
  }

  function newFolder() {
    setPrompt({
      title: t("新建文件夹"),
      initial: "",
      confirm: t("创建"),
      onSubmit: (name) => run(() => api.FilesMkdir(connId, joinPath(cwd, name))),
    });
  }

  function upload() {
    void run(async () => {
      const n = await api.FilesUpload(connId, cwd);
      if (n > 0) toast.success(t("已上传 {0} 个文件", n));
    });
  }

  function onKeyDown(ev: React.KeyboardEvent) {
    // 对话框与菜单经 portal 渲染, 键盘事件仍会沿 React 树冒泡到这里, 只处理本面板 DOM 内的按键。
    if (!ev.currentTarget.contains(ev.target as Node) || ev.target instanceof HTMLInputElement || !listing) return;
    const key = ev.key.toLowerCase();
    const single = selectedEntries.length === 1 ? selectedEntries[0] : null;
    let handled = true;
    if ((ev.ctrlKey || ev.metaKey) && key === "a") setSelected(entries.map((e) => e.path));
    else if ((ev.ctrlKey || ev.metaKey) && key === "c") toClipboard("copy", selectedEntries);
    else if ((ev.ctrlKey || ev.metaKey) && key === "x") toClipboard("cut", selectedEntries);
    else if ((ev.ctrlKey || ev.metaKey) && key === "v") void paste(cwd);
    else if (key === "f2" && single) rename(single);
    else if (key === "enter" && single) open(single);
    else if (key === "delete" && selectedEntries.length > 0) setConfirmRemove(selectedEntries);
    else if (key === "backspace" && cwd !== "/") void load(parentPath(cwd));
    else if (key === "escape") setSelected([]);
    else handled = false;
    if (handled) ev.preventDefault();
  }

  const single = menuTarget && selectedEntries.length <= 1 ? menuTarget : null;
  const targets = menuTarget ? selectedEntries : [];

  return (
    <div className="flex h-full min-h-0 flex-col outline-none" tabIndex={0} onKeyDown={onKeyDown}>
      <div className="flex shrink-0 items-center gap-1 border-b px-3 py-2">
        <Button variant="ghost" size="icon-sm" aria-label={t("上级目录")} disabled={!cwd || cwd === "/"} onClick={() => load(parentPath(cwd))}>
          <ArrowUpIcon />
        </Button>
        <form
          className="min-w-0 flex-1"
          onSubmit={(e) => {
            e.preventDefault();
            void load(pathInput);
          }}
        >
          <Input className="h-7 font-mono text-xs" value={pathInput} onChange={(e) => setPathInput(e.target.value)} />
        </form>
        <Button variant="ghost" size="icon-sm" aria-label={t("刷新")} onClick={() => load(cwd)}>
          {loading ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />}
        </Button>
        {clipboard && (
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t("粘贴 {0} 项到当前目录", clipboard.paths.length)}
            title={t("粘贴 {0} 项到当前目录", clipboard.paths.length)}
            disabled={!listing}
            onClick={() => paste(cwd)}
          >
            <ClipboardPasteIcon />
          </Button>
        )}
        <Button variant="ghost" size="icon-sm" aria-label={t("新建文件夹")} disabled={!listing} onClick={newFolder}>
          <FolderPlusIcon />
        </Button>
        <Button variant="ghost" size="icon-sm" aria-label={t("上传文件")} disabled={!listing} onClick={upload}>
          <UploadIcon />
        </Button>
      </div>

      {error && <p className="text-destructive shrink-0 px-4 py-3 text-sm">{errorText(error)}</p>}

      <ContextMenu>
        <ContextMenuTrigger asChild>
          <div
            className="czl-scroll min-h-0 flex-1 overflow-auto"
            onClick={(ev) => {
              if (!(ev.target as HTMLElement).closest("tr[data-path]")) setSelected([]);
            }}
            onContextMenu={(ev) => {
              const row = (ev.target as HTMLElement).closest<HTMLElement>("tr[data-path]");
              const entry = row ? entries.find((e) => e.path === row.dataset.path) ?? null : null;
              setMenuTarget(entry);
              if (!entry) setSelected([]);
              else if (!selectedSet.has(entry.path)) {
                setSelected([entry.path]);
                setAnchor(entry.path);
              }
            }}
          >
            <table className="w-full text-sm">
              <thead className="bg-background text-muted-foreground sticky top-0 text-xs">
                <tr className="border-b">
                  <th className="px-3 py-1.5 text-left font-medium">{t("名称")}</th>
                  <th className="w-24 px-3 py-1.5 text-right font-medium">{t("大小")}</th>
                  <th className="w-40 px-3 py-1.5 text-left font-medium">{t("修改时间")}</th>
                  <th className="w-28 px-3 py-1.5 text-left font-medium">{t("权限")}</th>
                </tr>
              </thead>
              <tbody>
                {entries.map((e) => (
                  <tr
                    key={e.path}
                    data-path={e.path}
                    className={cn(
                      "cursor-default select-none",
                      selectedSet.has(e.path) ? "bg-muted" : "hover:bg-muted/50",
                      cutSet.has(e.path) && "opacity-50",
                    )}
                    onClick={(ev) => clickRow(e, ev)}
                    onDoubleClick={() => open(e)}
                  >
                    <td className="px-3 py-1">
                      <span className="flex min-w-0 items-center gap-2">
                        {e.isDir ? (
                          <FolderIcon className="text-accent size-4 shrink-0" />
                        ) : (
                          <FileIcon className="text-muted-foreground size-4 shrink-0" />
                        )}
                        <span className={cn("truncate", e.isLink && "italic")}>{e.name}</span>
                      </span>
                    </td>
                    <td className="text-muted-foreground px-3 py-1 text-right tabular-nums">{e.isDir ? "" : formatBytes(e.size)}</td>
                    <td className="text-muted-foreground px-3 py-1 tabular-nums">{formatTime(e.modTime)}</td>
                    <td className="text-muted-foreground px-3 py-1 font-mono text-xs">{e.mode}</td>
                  </tr>
                ))}
              </tbody>
            </table>
            {listing && entries.length === 0 && <p className="text-muted-foreground py-8 text-center text-sm">{t("空目录")}</p>}
          </div>
        </ContextMenuTrigger>
        {menuTarget ? (
          <ContextMenuContent>
            {single && (
              <ContextMenuItem onSelect={() => open(single)}>
                {single.isDir ? t("打开") : t("编辑")}
                <ContextMenuShortcut>Enter</ContextMenuShortcut>
              </ContextMenuItem>
            )}
            {single && !single.isDir && <ContextMenuItem onSelect={() => download(single)}>{t("下载…")}</ContextMenuItem>}
            {single && <ContextMenuSeparator />}
            <ContextMenuItem onSelect={() => toClipboard("copy", targets)}>
              {t("复制")}
              <ContextMenuShortcut>{mod}C</ContextMenuShortcut>
            </ContextMenuItem>
            <ContextMenuItem onSelect={() => toClipboard("cut", targets)}>
              {t("剪切")}
              <ContextMenuShortcut>{mod}X</ContextMenuShortcut>
            </ContextMenuItem>
            {single?.isDir && clipboard && (
              <ContextMenuItem onSelect={() => paste(single.path)}>{t("粘贴到此文件夹")}</ContextMenuItem>
            )}
            <ContextMenuSeparator />
            {single && (
              <ContextMenuItem onSelect={() => rename(single)}>
                {t("重命名")}
                <ContextMenuShortcut>F2</ContextMenuShortcut>
              </ContextMenuItem>
            )}
            <ContextMenuItem onSelect={() => moveTo(targets)}>{t("移动到…")}</ContextMenuItem>
            <ContextMenuItem onSelect={() => changeMode(targets)}>{t("修改权限…")}</ContextMenuItem>
            <ContextMenuItem onSelect={() => copyPath(targets.map((e) => e.path))}>{t("复制路径")}</ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem className="text-destructive focus:text-destructive" onSelect={() => setConfirmRemove(targets)}>
              {targets.length > 1 ? t("删除 {0} 项", targets.length) : t("删除")}
              <ContextMenuShortcut>Del</ContextMenuShortcut>
            </ContextMenuItem>
          </ContextMenuContent>
        ) : (
          <ContextMenuContent>
            <ContextMenuItem disabled={!clipboard || !listing} onSelect={() => paste(cwd)}>
              {clipboard ? t("粘贴 {0} 项", clipboard.paths.length) : t("粘贴")}
              <ContextMenuShortcut>{mod}V</ContextMenuShortcut>
            </ContextMenuItem>
            <ContextMenuSeparator />
            <ContextMenuItem disabled={!listing} onSelect={newFolder}>{t("新建文件夹")}</ContextMenuItem>
            <ContextMenuItem disabled={!listing} onSelect={upload}>{t("上传文件")}</ContextMenuItem>
            <ContextMenuItem onSelect={() => load(cwd)}>{t("刷新")}</ContextMenuItem>
            <ContextMenuItem disabled={!cwd} onSelect={() => copyPath([cwd])}>{t("复制当前路径")}</ContextMenuItem>
          </ContextMenuContent>
        )}
      </ContextMenu>

      <PromptDialog request={prompt} onClose={() => setPrompt(null)} />
      <ChmodDialog request={chmod} onClose={() => setChmod(null)} />
      <AlertDialog open={!!confirmRemove} onOpenChange={(o) => !o && setConfirmRemove(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {confirmRemove && confirmRemove.length > 1
                ? t("删除所选 {0} 项?", confirmRemove.length)
                : t("删除 {0}?", confirmRemove?.[0]?.name)}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {confirmRemove?.some((e) => e.isDir)
                ? t("文件将从服务器上永久删除, 无法恢复。目录只能在为空时删除。")
                : t("文件将从服务器上永久删除, 无法恢复。")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const items = confirmRemove;
                setConfirmRemove(null);
                if (items?.length) remove(items);
              }}
            >
              {t("删除")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
