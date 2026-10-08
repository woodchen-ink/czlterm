"use client";

import { useEffect, useState } from "react";
import { KeyRoundIcon, LockKeyholeIcon, RefreshCwIcon, XIcon } from "lucide-react";
import { toast } from "sonner";

import { UnlockCancelled, useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { api, type VaultItem } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

/**
 * 选择 Vaultwarden 条目。kind 决定只列能提供密码的条目还是能提供私钥的条目。
 * 只拿到条目摘要 (名称、用户名), 密码与私钥始终留在后端。
 */
export function VaultItemPicker({
  kind,
  value,
  valueName,
  onChange,
}: {
  kind: "password" | "key";
  value: string;
  valueName: string;
  onChange: (id: string, item?: VaultItem) => void;
}) {
  const { withVault } = useVault();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [items, setItems] = useState<VaultItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [syncing, setSyncing] = useState(false);
  // 从服务器同步后加一, 让列表重新查询。
  const [reloadKey, setReloadKey] = useState(0);

  async function syncFromServer() {
    setSyncing(true);
    try {
      await withVault(() => api.VaultRefresh());
      setReloadKey((k) => k + 1);
      toast.success("已从服务器同步保险库");
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    } finally {
      setSyncing(false);
    }
  }

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    const t = setTimeout(async () => {
      setLoading(true);
      try {
        const list = await withVault(() => api.VaultSearch(query));
        if (!cancelled) setItems(list.filter((i) => (kind === "key" ? i.hasKey : i.hasPassword)));
      } catch (e) {
        if (e instanceof UnlockCancelled) setOpen(false);
        else toast.error(errorText(e));
      } finally {
        if (!cancelled) setLoading(false);
      }
    }, 150);
    return () => {
      cancelled = true;
      clearTimeout(t);
    };
  }, [open, query, kind, withVault, reloadKey]);

  const label = value ? valueName || "已选条目 (保险库锁定中)" : "未选择";

  return (
    <>
      <div className="flex gap-1">
        <Button type="button" variant="outline" className="min-w-0 flex-1 justify-start font-normal" onClick={() => setOpen(true)}>
          {kind === "key" ? <KeyRoundIcon /> : <LockKeyholeIcon />}
          <span className={cn("truncate", !value && "text-muted-foreground")}>{label}</span>
        </Button>
        {value && (
          <Button type="button" variant="ghost" size="icon" aria-label="清除" onClick={() => onChange("")}>
            <XIcon />
          </Button>
        )}
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{kind === "key" ? "选择私钥条目" : "选择密码条目"}</DialogTitle>
          </DialogHeader>
          <div className="flex gap-1">
            <Input autoFocus placeholder="搜索名称或用户名" value={query} onChange={(e) => setQuery(e.target.value)} />
            <Button
              type="button"
              variant="outline"
              size="icon"
              aria-label="从服务器同步"
              title="从服务器同步 (找不到刚在 Bitwarden 里新加的条目时点这里)"
              onClick={syncFromServer}
              disabled={syncing}
            >
              <RefreshCwIcon className={syncing ? "animate-spin" : ""} />
            </Button>
          </div>
          <div className="czl-scroll -mx-2 max-h-80 overflow-y-auto">
            {items.map((it) => (
              <button
                key={it.id}
                type="button"
                className={cn(
                  "hover:bg-muted flex w-full flex-col rounded-md px-2 py-1.5 text-left",
                  it.id === value && "bg-muted",
                )}
                onClick={() => {
                  onChange(it.id, it);
                  setOpen(false);
                }}
              >
                <span className="truncate text-sm">{it.name}</span>
                <span className="text-muted-foreground truncate text-xs">
                  {[it.username, it.hasKey ? "私钥" : "", it.hasPassword ? "密码" : ""].filter(Boolean).join(" · ")}
                </span>
              </button>
            ))}
            {!loading && items.length === 0 && (
              <p className="text-muted-foreground px-2 py-6 text-center text-sm">
                {kind === "key" ? "没有含私钥的条目 (SSH 密钥条目, 或带 private_key 字段的条目)" : "没有含密码的登录条目"}
                <br />
                刚新加的条目找不到? 点搜索框右边的同步按钮。
              </p>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </>
  );
}
