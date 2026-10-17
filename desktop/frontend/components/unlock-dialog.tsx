"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useState } from "react";
import { KeyRoundIcon, Loader2Icon } from "lucide-react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { api, type VaultStatus } from "@/lib/api";
import { errorText } from "@/lib/errors";

/** 保险库解锁框。未安装 bw 或未登录时给出下一步操作, 而不是让用户对着密码框猜。 */
export function UnlockDialog({
  open,
  status,
  onOpenChange,
  onUnlocked,
}: {
  open: boolean;
  status: VaultStatus | null;
  onOpenChange: (open: boolean) => void;
  onUnlocked: () => void;
}) {
  useLanguage();
  const [password, setPassword] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const state = status?.status ?? "unavailable";

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      await api.VaultUnlock(password);
      setPassword("");
      onUnlocked();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          setPassword("");
          setError("");
        }
        onOpenChange(o);
      }}
    >
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRoundIcon className="size-4" /> {t("解锁保险库")}</DialogTitle>
          <DialogDescription>
            {status?.userEmail ? `${status.userEmail} · ${status.serverUrl || "bitwarden.com"}` : t("通过 bw 命令行读取 Vaultwarden 中的密码与私钥")}
          </DialogDescription>
        </DialogHeader>

        {state === "unavailable" && (
          <div className="bg-secondary text-secondary-foreground space-y-2 rounded-md p-3 text-sm">
            <p>{status?.error ? errorText(status.error) : t("没有找到 bw 命令行工具。")}</p>
            <p className="text-muted-foreground">{t("安装后在终端里登录一次, 再回到这里解锁:")}
            </p>
            <pre className="bg-background overflow-x-auto rounded p-2 font-mono text-xs">
              {"npm i -g @bitwarden/cli\nbw config server https://your-vaultwarden\nbw login"}
            </pre>
          </div>
        )}
        {state === "unauthenticated" && (
          <div className="bg-secondary text-secondary-foreground space-y-2 rounded-md p-3 text-sm">
            <p>{t("bw 还没有登录。请在终端里执行 (涉及两步验证, 需要你亲自完成):")}
            </p>
            <pre className="bg-background overflow-x-auto rounded p-2 font-mono text-xs">
              {"bw config server https://your-vaultwarden\nbw login"}
            </pre>
          </div>
        )}

        {(state === "locked" || state === "unlocked") && (
          <form id="unlock-form" onSubmit={submit} className="space-y-2">
            <Input
              type="password"
              autoFocus
              placeholder={t("主密码")}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              disabled={busy}
            />
            {error && <p className="text-destructive text-sm">{errorText(error)}</p>}
            <p className="text-muted-foreground text-xs">{t("主密码只交给 bw 解锁, 不保存。可在设置中开启记住解锁，将会话密钥保存在系统钥匙串。")}
            </p>
          </form>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            {t("取消")}
            </Button>
          {(state === "locked" || state === "unlocked") && (
            <Button type="submit" form="unlock-form" disabled={busy || !password}>
              {busy && <Loader2Icon className="animate-spin" />} {t("解锁")}
              </Button>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
