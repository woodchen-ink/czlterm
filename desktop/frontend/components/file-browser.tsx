"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ArrowUpIcon,
  FileIcon,
  FolderIcon,
  FolderPlusIcon,
  Loader2Icon,
  RefreshCwIcon,
  UploadIcon,
} from "lucide-react";
import { toast } from "sonner";

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
  ContextMenuTrigger,
} from "@/components/ui/context-menu";
import { Input } from "@/components/ui/input";
import { api, type Entry, type Listing } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes, formatTime, joinPath, parentPath } from "@/lib/format";
import { cn } from "@/lib/utils";

/** SFTP 文件管理。双击目录进入, 双击文件用外部编辑器打开, 保存后自动传回。 */
export function FileBrowser({ connId }: { connId: string }) {
  const { withVault } = useVault();
  const [listing, setListing] = useState<Listing | null>(null);
  const [pathInput, setPathInput] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [selected, setSelected] = useState("");
  const [prompt, setPrompt] = useState<PromptRequest | null>(null);
  const [confirmRemove, setConfirmRemove] = useState<Entry | null>(null);

  const load = useCallback(
    async (dir: string) => {
      setLoading(true);
      setError("");
      try {
        const l = await withVault(() => api.FilesList(connId, dir));
        setListing(l);
        setPathInput(l.path);
        setSelected("");
      } catch (e) {
        if (!(e instanceof UnlockCancelled)) setError(errorText(e));
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

  /** 执行一次文件操作, 成功后刷新当前目录。 */
  async function run(fn: () => Promise<unknown>, success?: string) {
    try {
      await withVault(fn);
      if (success) toast.success(success);
      await load(cwd);
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    }
  }

  async function edit(entry: Entry) {
    try {
      await withVault(() => api.FilesEdit(connId, entry.path));
      toast.info(`已在编辑器中打开 ${entry.name}, 保存后自动上传`);
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    }
  }

  async function download(entry: Entry) {
    try {
      const saved = await withVault(() => api.FilesDownload(connId, entry.path));
      if (saved) toast.success(`已下载到 ${saved}`);
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    }
  }

  function open(entry: Entry) {
    if (entry.isDir) void load(entry.path);
    else void edit(entry);
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-1 border-b px-3 py-2">
        <Button variant="ghost" size="icon-sm" aria-label="上级目录" disabled={!cwd || cwd === "/"} onClick={() => load(parentPath(cwd))}>
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
        <Button variant="ghost" size="icon-sm" aria-label="刷新" onClick={() => load(cwd)}>
          {loading ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />}
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="新建文件夹"
          disabled={!listing}
          onClick={() =>
            setPrompt({
              title: "新建文件夹",
              initial: "",
              confirm: "创建",
              onSubmit: (name) => run(() => api.FilesMkdir(connId, joinPath(cwd, name))),
            })
          }
        >
          <FolderPlusIcon />
        </Button>
        <Button
          variant="ghost"
          size="icon-sm"
          aria-label="上传文件"
          disabled={!listing}
          onClick={() =>
            run(async () => {
              const n = await api.FilesUpload(connId, cwd);
              if (n > 0) toast.success(`已上传 ${n} 个文件`);
            })
          }
        >
          <UploadIcon />
        </Button>
      </div>

      {error && <p className="text-destructive shrink-0 px-4 py-3 text-sm">{error}</p>}

      <div className="czl-scroll min-h-0 flex-1 overflow-auto">
        <table className="w-full text-sm">
          <thead className="bg-background text-muted-foreground sticky top-0 text-xs">
            <tr className="border-b">
              <th className="px-3 py-1.5 text-left font-medium">名称</th>
              <th className="w-24 px-3 py-1.5 text-right font-medium">大小</th>
              <th className="w-40 px-3 py-1.5 text-left font-medium">修改时间</th>
              <th className="w-28 px-3 py-1.5 text-left font-medium">权限</th>
            </tr>
          </thead>
          <tbody>
            {listing?.entries.map((e) => (
              <ContextMenu key={e.path}>
                <ContextMenuTrigger asChild>
                  <tr
                    className={cn("cursor-default select-none", selected === e.path ? "bg-muted" : "hover:bg-muted/50")}
                    onClick={() => setSelected(e.path)}
                    onContextMenu={() => setSelected(e.path)}
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
                </ContextMenuTrigger>
                <ContextMenuContent>
                  <ContextMenuItem onSelect={() => open(e)}>{e.isDir ? "打开" : "编辑"}</ContextMenuItem>
                  {!e.isDir && <ContextMenuItem onSelect={() => download(e)}>下载…</ContextMenuItem>}
                  <ContextMenuSeparator />
                  <ContextMenuItem
                    onSelect={() =>
                      setPrompt({
                        title: "重命名",
                        initial: e.name,
                        confirm: "重命名",
                        onSubmit: (name) => run(() => api.FilesRename(connId, e.path, joinPath(cwd, name))),
                      })
                    }
                  >
                    重命名
                  </ContextMenuItem>
                  <ContextMenuItem className="text-destructive focus:text-destructive" onSelect={() => setConfirmRemove(e)}>
                    删除
                  </ContextMenuItem>
                </ContextMenuContent>
              </ContextMenu>
            ))}
          </tbody>
        </table>
        {listing && listing.entries.length === 0 && <p className="text-muted-foreground py-8 text-center text-sm">空目录</p>}
      </div>

      <PromptDialog request={prompt} onClose={() => setPrompt(null)} />
      <AlertDialog open={!!confirmRemove} onOpenChange={(o) => !o && setConfirmRemove(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除 {confirmRemove?.name}?</AlertDialogTitle>
            <AlertDialogDescription>
              {confirmRemove?.isDir ? "只能删除空目录。" : "文件将从服务器上永久删除, 无法恢复。"}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const target = confirmRemove;
                setConfirmRemove(null);
                if (target) void run(() => api.FilesRemove(connId, target.path), `已删除 ${target.name}`);
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
