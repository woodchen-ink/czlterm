"use client";

import { useEffect, useState } from "react";
import {
  ArrowLeftIcon,
  BotIcon,
  CopyIcon,
  FolderOpenIcon,
  GitBranchIcon,
  InfoIcon,
  KeyRoundIcon,
  Loader2Icon,
  RefreshCwIcon,
  SquareTerminalIcon,
} from "lucide-react";
import { toast } from "sonner";

import { AboutPanel } from "@/components/about-panel";
import { useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { Textarea } from "@/components/ui/textarea";
import { api, type MCPConfig, type Settings, type SettingsView } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

export type SettingsTab = "apps" | "vault" | "sync" | "mcp" | "about";

const sections: { id: SettingsTab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { id: "apps", label: "外部程序", icon: SquareTerminalIcon },
  { id: "vault", label: "保险库", icon: KeyRoundIcon },
  { id: "sync", label: "同步", icon: GitBranchIcon },
  { id: "mcp", label: "MCP", icon: BotIcon },
  { id: "about", label: "关于", icon: InfoIcon },
];

/** 整页设置: 左侧分区导航, 右侧内容独立滚动, 底部保存条固定。 */
export function SettingsPage({ initialTab = "apps", onClose, onSaved }: { initialTab?: SettingsTab; onClose: () => void; onSaved: () => void }) {
  const [tab, setTab] = useState<SettingsTab>(initialTab);
  const [view, setView] = useState<SettingsView | null>(null);
  const [form, setForm] = useState<Settings | null>(null);
  const [gitSecret, setGitSecret] = useState("");
  const [clearGitSecret, setClearGitSecret] = useState(false);
  const [mcp, setMcp] = useState<MCPConfig | null>(null);
  const [busy, setBusy] = useState(false);
  const { status, refreshStatus } = useVault();

  useEffect(() => {
    api
      .GetSettings()
      .then((v) => {
        setView(v);
        setForm({ ...v.settings });
      })
      .catch((e) => toast.error(errorText(e)));
    api.GetMCPConfig().then(setMcp).catch(() => setMcp(null));
    queueMicrotask(() => void refreshStatus());
  }, [refreshStatus]);

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => setForm((f) => (f ? { ...f, [k]: v } : f));

  async function save() {
    if (!form) return;
    setBusy(true);
    try {
      await api.SaveSettings(form);
      if (gitSecret.trim() || clearGitSecret) await api.SetGitSecret(clearGitSecret ? "" : gitSecret);
      setMcp(await api.GetMCPConfig());
      toast.success("设置已保存");
      onSaved();
      onClose();
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  function copy(text: string) {
    navigator.clipboard.writeText(text).then(
      () => toast.success("已复制"),
      () => toast.error("复制失败"),
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          <ArrowLeftIcon /> 返回
        </Button>
        <h1 className="text-sm font-semibold">设置</h1>
      </div>

      <div className="flex min-h-0 flex-1">
        <nav className="bg-sidebar w-48 shrink-0 space-y-0.5 border-r p-2">
          {sections.map((s) => (
            <button
              key={s.id}
              type="button"
              onClick={() => setTab(s.id)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm",
                tab === s.id ? "bg-sidebar-accent text-sidebar-accent-foreground" : "hover:bg-sidebar-accent/60",
              )}
            >
              <s.icon className="size-4" />
              {s.label}
            </button>
          ))}
        </nav>

        <div className="czl-scroll min-w-0 flex-1 overflow-y-auto">
          {!view || !form ? (
            <div className="text-muted-foreground flex h-full items-center justify-center">
              <Loader2Icon className="animate-spin" />
            </div>
          ) : (
            <div className="mx-auto max-w-2xl space-y-5 p-6">
              {tab === "apps" && (
                <>
                  <Field label="终端" hint="SSH 连接在这里打开">
                    <Select value={form.terminal} onValueChange={(v) => set("terminal", v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {view.terminals.map((t) => (
                          <SelectItem key={t.id} value={t.id} disabled={!t.installed}>
                            {t.name}
                            {!t.installed && " (未安装)"}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label="编辑器" hint="在文件管理中编辑远程文件时使用, 保存后自动上传">
                    <Select value={form.editor} onValueChange={(v) => set("editor", v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {view.editors.map((t) => (
                          <SelectItem key={t.id} value={t.id} disabled={!t.installed}>
                            {t.name}
                            {!t.installed && " (未安装)"}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label="自定义编辑器路径" hint="填写后忽略上面的选择">
                    <Input value={form.editorPath} placeholder="可留空" onChange={(e) => set("editorPath", e.target.value)} />
                  </Field>
                  <p className="text-muted-foreground text-sm">
                    RDP 使用
                    {view.os === "darwin"
                      ? " Windows App (App Store)。它不接受外部传入的密码, 连接时密码复制到剪贴板, 45 秒后清除。"
                      : "系统远程桌面 (mstsc), 密码自动填入。"}
                  </p>
                  {view.os !== "darwin" && (
                    <Field label="VNC 查看器路径" hint="留空时自动查找 TigerVNC; 其它查看器无法自动填密码, 会改用剪贴板">
                      <Input
                        value={form.vncViewerPath}
                        placeholder="C:\Program Files\TigerVNC\vncviewer.exe"
                        onChange={(e) => set("vncViewerPath", e.target.value)}
                      />
                    </Field>
                  )}
                </>
              )}

              {tab === "vault" && (
                <>
                  <div className="bg-secondary space-y-1 rounded-md p-3 text-sm break-all">
                    <p>
                      状态: <span className="font-medium">{status?.status ?? "未知"}</span>
                      {status?.userEmail && ` · ${status.userEmail}`}
                    </p>
                    {status?.serverUrl && <p className="text-muted-foreground text-xs">{status.serverUrl}</p>}
                    {status?.bwPath && <p className="text-muted-foreground font-mono text-xs">{status.bwPath}</p>}
                    {status?.error && <p className="text-destructive text-xs">{errorText(status.error)}</p>}
                    {status?.status === "unlocked" && (
                      <Button
                        size="sm"
                        variant="outline"
                        className="mt-2"
                        onClick={() =>
                          api.VaultRefresh().then(
                            () => toast.success("已从服务器同步保险库"),
                            (e) => toast.error(errorText(e)),
                          )
                        }
                      >
                        <RefreshCwIcon /> 从服务器同步条目
                      </Button>
                    )}
                  </div>
                  <Field label="bw 路径" hint="留空时在 PATH 中查找">
                    <Input value={form.bwPath} placeholder="/opt/homebrew/bin/bw" onChange={(e) => set("bwPath", e.target.value)} />
                  </Field>
                  <ToggleRow
                    label="记住解锁"
                    hint="把 bw 会话密钥存进系统钥匙串, 重启 czlterm 不用再输主密码; 手动锁定或空闲超时锁定时会一并清除"
                    checked={form.vaultRemember}
                    onChange={(v) => set("vaultRemember", v)}
                  />
                  <Field label="空闲自动锁定 (分钟)" hint="0 表示直到退出程序">
                    <Input
                      inputMode="numeric"
                      value={form.vaultAutoLockMinutes}
                      onChange={(e) => set("vaultAutoLockMinutes", Number(e.target.value.replace(/\D/g, "")) || 0)}
                    />
                  </Field>
                </>
              )}

              {tab === "sync" && (
                <>
                  <p className="text-muted-foreground text-sm">
                    连接配置 (不含任何密码) 存放在数据目录, 一连接一个 JSON 文件, 用 git 同步到你自己的私有仓库。
                  </p>
                  <Field label="仓库地址" hint="如 git@github.com:you/czlterm-data.git 或 https://…; 留空则只做本地提交">
                    <Input value={form.gitRemote} onChange={(e) => set("gitRemote", e.target.value)} />
                  </Field>
                  <Field label="用户名 (可选)" hint="HTTPS 仓库使用; SSH 仓库的用户名写在地址里。留空用 git 默认认证">
                    <Input value={form.gitUsername} autoComplete="off" onChange={(e) => set("gitUsername", e.target.value)} />
                  </Field>
                  <Field
                    label="密钥 (可选)"
                    hint="HTTPS 仓库填访问令牌或密码, SSH 仓库粘贴私钥。保存在系统钥匙串, 不写入设置文件; 留空用 git 默认认证"
                  >
                    <Textarea
                      rows={3}
                      className="font-mono text-xs"
                      autoComplete="off"
                      spellCheck={false}
                      value={gitSecret}
                      disabled={clearGitSecret}
                      placeholder={view.hasGitSecret ? "已保存 (留空保持不变)" : "未设置"}
                      onChange={(e) => setGitSecret(e.target.value)}
                    />
                    {view.hasGitSecret && (
                      <label className="text-muted-foreground flex items-center gap-2 text-xs">
                        <Switch checked={clearGitSecret} onCheckedChange={setClearGitSecret} /> 清除已保存的密钥
                      </label>
                    )}
                  </Field>
                  <ToggleRow label="修改后自动同步" checked={form.gitAutoSync} onChange={(v) => set("gitAutoSync", v)} />
                  <Button variant="outline" size="sm" onClick={() => api.RevealDataDir().catch((e) => toast.error(errorText(e)))}>
                    <FolderOpenIcon /> 打开数据目录
                  </Button>
                </>
              )}

              {tab === "mcp" && (
                <>
                  <ToggleRow label="启用 MCP" hint="在 127.0.0.1 上提供 MCP 端点, 以令牌鉴权; 只开这一项时 AI 只能列出连接" checked={form.mcpEnabled} onChange={(v) => set("mcpEnabled", v)} />
                  <ToggleRow
                    label="允许 AI 访问服务器"
                    hint="在全部 SSH 连接上列目录、读文件"
                    checked={form.mcpAllowAccess}
                    disabled={!form.mcpEnabled}
                    onChange={(v) => set("mcpAllowAccess", v)}
                  />
                  <ToggleRow
                    label="允许 AI 写文件"
                    checked={form.mcpAllowWrite}
                    disabled={!form.mcpEnabled || !form.mcpAllowAccess}
                    onChange={(v) => set("mcpAllowWrite", v)}
                  />
                  <ToggleRow
                    label="允许 AI 执行命令"
                    hint="支持多行脚本"
                    checked={form.mcpAllowExec}
                    disabled={!form.mcpEnabled || !form.mcpAllowAccess}
                    onChange={(v) => set("mcpAllowExec", v)}
                  />
                  {mcp && (
                    <>
                      <Snippet title="stdio 客户端 (Claude Desktop 等)" text={mcp.stdioJson} onCopy={copy} />
                      {mcp.httpJson ? (
                        <Snippet title="HTTP 客户端 (Claude Code 等, 含令牌)" text={mcp.httpJson} onCopy={copy} />
                      ) : (
                        <p className="text-muted-foreground text-xs">启用并保存后显示 HTTP 配置。</p>
                      )}
                      {mcp.running && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            api.RotateMCPToken().then(
                              (c) => {
                                setMcp(c);
                                toast.success("令牌已更换, 请更新 HTTP 客户端配置");
                              },
                              (e) => toast.error(errorText(e)),
                            )
                          }
                        >
                          更换令牌
                        </Button>
                      )}
                    </>
                  )}
                </>
              )}

              {tab === "about" && <AboutPanel />}
            </div>
          )}
        </div>
      </div>

      {tab !== "about" && (
        <div className="flex shrink-0 justify-end gap-2 border-t px-4 py-2">
          <Button variant="outline" onClick={onClose}>
            取消
          </Button>
          <Button onClick={save} disabled={busy || !form}>
            {busy && <Loader2Icon className="animate-spin" />} 保存
          </Button>
        </div>
      )}
    </div>
  );
}

function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <div className="space-y-1.5">
      <Label className="text-muted-foreground text-xs">{label}</Label>
      {children}
      {hint && <p className="text-muted-foreground text-xs">{hint}</p>}
    </div>
  );
}

function ToggleRow({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className={cn("flex items-start justify-between gap-4", disabled && "opacity-50")}>
      <span>
        <span className="block text-sm">{label}</span>
        {hint && <span className="text-muted-foreground block text-xs">{hint}</span>}
      </span>
      <Switch checked={checked && !disabled} disabled={disabled} onCheckedChange={onChange} />
    </label>
  );
}

/** 配置片段: 长路径在框内换行, 不撑宽页面。 */
function Snippet({ title, text, onCopy }: { title: string; text: string; onCopy: (t: string) => void }) {
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center justify-between">
        <Label className="text-muted-foreground text-xs">{title}</Label>
        <Button variant="ghost" size="icon-xs" aria-label="复制" onClick={() => onCopy(text)}>
          <CopyIcon />
        </Button>
      </div>
      <pre className="bg-secondary rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap">{text}</pre>
    </div>
  );
}
