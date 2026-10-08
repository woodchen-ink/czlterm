"use client";

import { useState } from "react";
import { Loader2Icon } from "lucide-react";

import { VaultItemPicker } from "@/components/vault-item-picker";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { api, defaultPort, type Connection, type ConnectionView, type Protocol } from "@/lib/api";
import { errorText } from "@/lib/errors";

const NO_JUMP = "__none";

/** 新建或编辑连接。凭据只引用 Vaultwarden 条目, 表单里不出现明文密码。 */
export function ConnectionForm({
  open,
  initial,
  all,
  groups,
  onOpenChange,
  onSaved,
}: {
  open: boolean;
  initial: ConnectionView | Connection | null;
  all: ConnectionView[];
  groups: string[];
  onOpenChange: (open: boolean) => void;
  onSaved: (c: Connection) => void;
}) {
  // 父组件每次打开时换 key 重新挂载, 初始值只在挂载时读取。
  const [form, setForm] = useState<Connection | null>(() => (initial ? { ...initial } : null));
  const [names, setNames] = useState(() => {
    const v = (initial ?? {}) as Partial<ConnectionView>;
    return { vault: v.vaultItemName ?? "", key: v.keyItemName ?? "" };
  });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  if (!form) return null;
  const proto = form.protocol as Protocol;
  const set = <K extends keyof Connection>(k: K, v: Connection[K]) => setForm((f) => (f ? { ...f, [k]: v } : f));
  const jumpCandidates = all.filter((c) => c.protocol === "ssh" && c.id !== form.id);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!form) return;
    setBusy(true);
    setError("");
    try {
      const saved = await api.SaveConnection({ ...form, port: Number(form.port) || 0 });
      onSaved(saved);
    } catch (err) {
      setError(errorText(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{form.id ? "编辑连接" : "新建连接"}</DialogTitle>
        </DialogHeader>
        <form id="conn-form" onSubmit={submit} className="czl-scroll -mx-1 max-h-[65vh] space-y-3 overflow-y-auto px-1">
          <div className="grid grid-cols-[7rem_1fr] gap-2">
            <Field label="协议">
              <Select value={proto} onValueChange={(v) => setForm((f) => (f ? { ...f, protocol: v, port: 0 } : f))}>
                <SelectTrigger className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="ssh">SSH</SelectItem>
                  <SelectItem value="rdp">RDP</SelectItem>
                  <SelectItem value="vnc">VNC</SelectItem>
                </SelectContent>
              </Select>
            </Field>
            <Field label="名称">
              <Input value={form.name} placeholder="默认用主机地址" onChange={(e) => set("name", e.target.value)} />
            </Field>
          </div>

          <div className="grid grid-cols-[1fr_6rem] gap-2">
            <Field label="主机">
              <Input value={form.host} required placeholder="example.com 或 10.0.0.5" onChange={(e) => set("host", e.target.value)} />
            </Field>
            <Field label="端口">
              <Input
                inputMode="numeric"
                value={form.port || ""}
                placeholder={String(defaultPort[proto] ?? "")}
                onChange={(e) => set("port", Number(e.target.value.replace(/\D/g, "")) || 0)}
              />
            </Field>
          </div>

          <div className="grid grid-cols-2 gap-2">
            <Field label="用户名">
              <Input value={form.username} placeholder="留空则用条目里的用户名" onChange={(e) => set("username", e.target.value)} />
            </Field>
            <Field label="分组">
              <Input list="conn-groups" value={form.group} placeholder="如 生产/数据库" onChange={(e) => set("group", e.target.value)} />
              <datalist id="conn-groups">
                {groups.map((g) => (
                  <option key={g} value={g} />
                ))}
              </datalist>
            </Field>
          </div>

          <Field label="凭据">
            <Select value={form.auth} onValueChange={(v) => set("auth", v)}>
              <SelectTrigger className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="system">{proto === "ssh" ? "系统默认 (~/.ssh、系统 agent)" : "不提供, 手动输入"}</SelectItem>
                <SelectItem value="vault">Vaultwarden 条目</SelectItem>
              </SelectContent>
            </Select>
          </Field>

          {form.auth === "vault" && (
            <div className="bg-secondary/50 space-y-3 rounded-md p-3">
              <Field label="密码条目">
                <VaultItemPicker
                  kind="password"
                  value={form.vaultItem}
                  valueName={names.vault}
                  onChange={(id, item) => {
                    set("vaultItem", id);
                    setNames((n) => ({ ...n, vault: item?.name ?? "" }));
                  }}
                />
              </Field>
              {proto === "ssh" && (
                <Field label="私钥条目" hint="SSH 密钥条目; 有口令时在条目里加 passphrase 字段">
                  <VaultItemPicker
                    kind="key"
                    value={form.keyItem}
                    valueName={names.key}
                    onChange={(id, item) => {
                      set("keyItem", id);
                      setNames((n) => ({ ...n, key: item?.name ?? "" }));
                    }}
                  />
                </Field>
              )}
            </div>
          )}

          {proto === "ssh" && (
            <>
              <Field label="跳板机">
                <Select value={form.jump || NO_JUMP} onValueChange={(v) => set("jump", v === NO_JUMP ? "" : v)}>
                  <SelectTrigger className="w-full">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value={NO_JUMP}>不使用</SelectItem>
                    {jumpCandidates.map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        {c.group ? `${c.group} / ${c.name}` : c.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </Field>
            </>
          )}

          <Field label="备注">
            <Textarea rows={2} value={form.notes} onChange={(e) => set("notes", e.target.value)} />
          </Field>
          {error && <p className="text-destructive text-sm">{error}</p>}
        </form>
        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)}>
            取消
          </Button>
          <Button type="submit" form="conn-form" disabled={busy}>
            {busy && <Loader2Icon className="animate-spin" />} 保存
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1">
      <Label className="text-muted-foreground text-xs">{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  );
}
