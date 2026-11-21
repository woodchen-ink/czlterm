"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useState } from "react";
import { EyeIcon, EyeOffIcon, Loader2Icon, ShieldAlertIcon } from "lucide-react";
import { toast } from "sonner";

import { FileTextInput } from "@/components/file-text-input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { api, type CredentialSummary } from "@/lib/api";
import { errorText } from "@/lib/errors";

/** 从粘贴或文件内容里取出私钥块 (OpenSSH / PEM)。整份 .pem、带注释的文件都能认。 */
export function extractPrivateKey(text: string): string | null {
  const m = text.match(/-----BEGIN [A-Z0-9 ]*PRIVATE KEY-----[\s\S]+?-----END [A-Z0-9 ]*PRIVATE KEY-----/);
  return m ? m[0] + "\n" : null;
}

/**
 * 新建或修改一条保存的凭据 (用户名、密码、SSH 私钥)。修改时不回显已存的秘密: 留空表示保持原值。
 * 私钥可粘贴、拖入或选择 id_ed25519 / .pem 文件; 保存时后端会解析一遍, 口令不对当场报错。
 */
export function CredentialDialog({
  open,
  initial,
  shared,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  /** 修改时传摘要, 新建为 null */
  initial: CredentialSummary | null;
  /** 当前是团队空间: 提示全体成员都能用这条凭据 */
  shared: boolean;
  onOpenChange: (o: boolean) => void;
  onSaved: (s: CredentialSummary) => void;
}) {
  useLanguage();
  const editing = !!initial;
  const [name, setName] = useState(initial?.name ?? "");
  const [username, setUsername] = useState(initial?.username ?? "");
  const [notes, setNotes] = useState(initial?.notes ?? "");
  const [password, setPassword] = useState("");
  const [showPassword, setShowPassword] = useState(false);
  const [privateKey, setPrivateKey] = useState("");
  const [passphrase, setPassphrase] = useState("");
  const [clearPassword, setClearPassword] = useState(false);
  const [clearKey, setClearKey] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  const hasPassword = (!!initial?.hasPassword && !clearPassword) || !!password;
  const hasKey = (!!initial?.hasKey && !clearKey) || !!privateKey.trim();

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const saved = await api.SaveCredential({
        id: initial?.id ?? "",
        name,
        username,
        notes,
        password,
        privateKey,
        passphrase,
        clearPassword,
        clearKey,
      });
      toast.success(editing ? t("凭据已更新") : t("凭据已保存"));
      onSaved(saved);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !busy && onOpenChange(o)}>
      <DialogContent className="sm:max-w-lg">
        <form onSubmit={submit} className="space-y-3">
          <DialogHeader>
            <DialogTitle>{editing ? t("编辑凭据") : t("新建凭据")}</DialogTitle>
            <DialogDescription>
              {t("不用 Vaultwarden 时, 把用户名、密码或 SSH 私钥加密保存在 czlterm 里。开启云同步后随空间端到端加密同步。")}
            </DialogDescription>
          </DialogHeader>

          {shared && (
            <div className="border-warning/40 bg-warning/10 flex gap-2 rounded-md border p-2.5 text-xs">
              <ShieldAlertIcon className="text-warning mt-0.5 size-4 shrink-0" />
              <span>{t("这是团队空间: 团队里所有成员 (包括查看者) 都能解密并使用这条凭据, 被移出的成员手里已同步的副本无法收回。移除成员后请更换相关服务器的密码与密钥。")}</span>
            </div>
          )}

          <div className="grid grid-cols-2 gap-2">
            <Field label={t("名称")}>
              <Input value={name} placeholder={t("如 生产服务器 root")} onChange={(e) => setName(e.target.value)} autoFocus={!editing} />
            </Field>
            <Field label={t("用户名")}>
              <Input value={username} autoComplete="off" placeholder={t("可选, 连接里没填时使用")} onChange={(e) => setUsername(e.target.value)} />
            </Field>
          </div>

          <Field label={t("密码")}>
            <div className="relative">
              <Input
                type={showPassword ? "text" : "password"}
                value={password}
                autoComplete="new-password"
                disabled={clearPassword}
                placeholder={initial?.hasPassword ? t("已保存, 留空保持不变") : t("可选")}
                className="pr-9"
                onChange={(e) => setPassword(e.target.value)}
              />
              <Button
                type="button"
                variant="ghost"
                size="icon-xs"
                className="absolute top-1/2 right-1.5 -translate-y-1/2"
                aria-label={showPassword ? t("隐藏") : t("显示")}
                onClick={() => setShowPassword((v) => !v)}
              >
                {showPassword ? <EyeOffIcon /> : <EyeIcon />}
              </Button>
            </div>
            {initial?.hasPassword && (
              <label className="text-muted-foreground flex items-center gap-2 text-xs">
                <Checkbox checked={clearPassword} onCheckedChange={(v) => setClearPassword(v === true)} /> {t("清除已保存的密码")}
              </label>
            )}
          </Field>

          <Field label={t("SSH 私钥")}>
            {initial?.hasKey && !privateKey && !clearKey && (
              <p className="text-muted-foreground text-xs">{t("已保存一把私钥; 粘贴或拖入新的私钥即替换。")}</p>
            )}
            <FileTextInput
              value={privateKey}
              onChange={setPrivateKey}
              rows={4}
              disabled={clearKey}
              extract={extractPrivateKey}
              invalidHint={t("没有找到私钥: 应以 -----BEGIN … PRIVATE KEY----- 开头。注意选的是私钥 (如 id_ed25519), 不是 .pub 公钥")}
              placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
            />
            {initial?.hasKey && (
              <label className="text-muted-foreground flex items-center gap-2 text-xs">
                <Checkbox checked={clearKey} onCheckedChange={(v) => setClearKey(v === true)} /> {t("清除已保存的私钥")}
              </label>
            )}
          </Field>

          {(privateKey.trim() || (initial?.hasKey && !clearKey)) && (
            <Field label={t("私钥口令")}>
              <Input
                type="password"
                value={passphrase}
                autoComplete="off"
                placeholder={privateKey.trim() ? t("私钥没加密就留空") : t("留空保持不变")}
                onChange={(e) => setPassphrase(e.target.value)}
              />
            </Field>
          )}

          <Field label={t("备注")}>
            <Input value={notes} onChange={(e) => setNotes(e.target.value)} />
          </Field>

          {error && <p className="text-destructive text-sm">{error}</p>}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={busy}>
              {t("取消")}
            </Button>
            <Button type="submit" disabled={busy || (!hasPassword && !hasKey)}>
              {busy && <Loader2Icon className="animate-spin" />} {t("保存")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1.5">
      <Label className="text-muted-foreground text-xs">{label}</Label>
      {children}
    </div>
  );
}
