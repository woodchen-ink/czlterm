"use client";

import { normalizeLanguage, setLanguage, t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useEffect, useState } from "react";
import {
  ArrowLeftIcon,
  BotIcon,
  CircleUserIcon,
  CloudIcon,
  CopyIcon,
  InfoIcon,
  KeySquareIcon,
  LanguagesIcon,
  KeyRoundIcon,
  Loader2Icon,
  RefreshCwIcon,
  SquareTerminalIcon,
  UsersIcon,
} from "lucide-react";
import { toast } from "sonner";

import { AboutPanel } from "@/components/about-panel";
import { AccountPanel } from "@/components/cloud/account-panel";
import { SyncPanel } from "@/components/cloud/sync-panel";
import { TeamPanel } from "@/components/cloud/team-panel";
import { CredentialsPanel } from "@/components/credentials-panel";
import { useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { api, openExternal, type MCPConfig, type Settings, type SettingsView } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

export type SettingsTab = "general" | "account" | "sync" | "teams" | "credentials" | "apps" | "vault" | "mcp" | "about";

const sections: { id: SettingsTab; label: string; icon: React.ComponentType<{ className?: string }> }[] = [
  { id: "general", label: "通用", icon: LanguagesIcon },
  { id: "account", label: "账号", icon: CircleUserIcon },
  { id: "sync", label: "云同步", icon: CloudIcon },
  { id: "teams", label: "团队", icon: UsersIcon },
  { id: "credentials", label: "凭据", icon: KeySquareIcon },
  { id: "apps", label: "外部程序", icon: SquareTerminalIcon },
  { id: "vault", label: "保险库", icon: KeyRoundIcon },
  { id: "mcp", label: "MCP", icon: BotIcon },
  { id: "about", label: "关于", icon: InfoIcon },
];

/** 这些分区自己处理保存 (即时生效), 底部不显示保存条。 */
const selfSaving: SettingsTab[] = ["general", "account", "sync", "teams", "credentials", "about"];

/** 整页设置: 左侧分区导航, 右侧内容独立滚动, 底部保存条固定。 */
export function SettingsPage({
  initialTab = "general",
  onClose,
  onSaved,
}: {
  initialTab?: SettingsTab;
  onClose: () => void;
  onSaved: () => void;
}) {
  const language = useLanguage();
  const [tab, setTab] = useState<SettingsTab>(initialTab);
  const [view, setView] = useState<SettingsView | null>(null);
  const [form, setForm] = useState<Settings | null>(null);
  const [mcp, setMcp] = useState<MCPConfig | null>(null);
  const [busy, setBusy] = useState(false);
  // 自启状态读自系统登记, 切换即生效, 不随底部保存提交。
  const [autoStart, setAutoStart] = useState(false);
  const { status, refreshStatus } = useVault();

  useEffect(() => {
    api
      .GetSettings()
      .then((v) => {
        setView(v);
        setForm({ ...v.settings });
        setAutoStart(v.autoStart);
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
      setMcp(await api.GetMCPConfig());
      toast.success(t("设置已保存"));
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
      () => toast.success(t("已复制")),
      () => toast.error(t("复制失败")),
    );
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <Button variant="ghost" size="sm" onClick={onClose}>
          <ArrowLeftIcon /> {t("返回")}
          </Button>
        <h1 className="text-sm font-semibold">{t("设置")}</h1>
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
                tab === s.id ? "bg-brand-soft text-brand-strong font-medium" : "hover:bg-sidebar-accent/60",
              )}
            >
              <s.icon className="size-4" />
              {t(s.label)}
            </button>
          ))}
        </nav>

        <div className="czl-scroll min-w-0 flex-1 overflow-y-auto">
          {tab === "general" ? (
            <div className="mx-auto flex max-w-2xl flex-col gap-5 p-6">
              <Field label={t("语言 / Language")} hint={t("立即生效，仅保存在本机")}>
                <Select value={language} onValueChange={(value) => setLanguage(normalizeLanguage(value))}>
                  <SelectTrigger className="w-full" aria-label={t("语言 / Language")}><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectGroup>
                      <SelectItem value="system">{t("跟随系统")}</SelectItem>
                      <SelectItem value="zh-CN">简体中文</SelectItem>
                      <SelectItem value="en">English</SelectItem>
                    </SelectGroup>
                  </SelectContent>
                </Select>
              </Field>
              {view?.autoStartSupported && (
                <ToggleRow
                  label={t("开机自启")}
                  hint={t("登录系统后在后台启动, 窗口收在托盘; 立即生效")}
                  checked={autoStart}
                  onChange={(on) =>
                    api
                      .SetAutoStart(on)
                      .then(setAutoStart)
                      .catch((e) => toast.error(errorText(e)))
                  }
                />
              )}
            </div>
          ) : tab === "account" ? (
            <div className="mx-auto max-w-2xl p-6">
              <AccountPanel onOpenSync={() => setTab("sync")} onOpenTeams={() => setTab("teams")} />
            </div>
          ) : tab === "sync" ? (
            <div className="mx-auto max-w-3xl p-6">
              <SyncPanel onOpenTeams={() => setTab("teams")} />
            </div>
          ) : tab === "teams" ? (
            <div className="mx-auto max-w-2xl p-6">
              <TeamPanel onOpenAccount={() => setTab("account")} onOpenSync={() => setTab("sync")} />
            </div>
          ) : tab === "credentials" ? (
            <div className="mx-auto max-w-2xl p-6">
              <CredentialsPanel />
            </div>
          ) : !view || !form ? (
            <div className="text-muted-foreground flex h-full items-center justify-center">
              <Loader2Icon className="animate-spin" />
            </div>
          ) : (
            <div className="mx-auto max-w-2xl space-y-5 p-6">
              {tab === "apps" && (
                <>
                  <Field label={t("终端")} hint={t("SSH 连接在这里打开")}>
                    <Select value={form.terminal} onValueChange={(v) => set("terminal", v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {view.terminals.map((terminal) => (
                          <SelectItem key={terminal.id} value={terminal.id} disabled={!terminal.installed}>
                            {terminal.id === "builtin" ? t("内置终端") : terminal.name}
                            {!terminal.installed && t(" (未安装)")}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label={t("编辑器")} hint={t("在文件管理中编辑远程文件时使用, 保存后自动上传")}>
                    <Select value={form.editor} onValueChange={(v) => set("editor", v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {view.editors.map((editor) => (
                          <SelectItem key={editor.id} value={editor.id} disabled={!editor.installed}>
                            {editor.name}
                            {!editor.installed && t(" (未安装)")}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </Field>
                  <Field label={t("自定义编辑器路径")} hint={t("填写后忽略上面的选择")}>
                    <Input value={form.editorPath} placeholder={t("可留空")} onChange={(e) => set("editorPath", e.target.value)} />
                  </Field>
                  <p className="text-muted-foreground text-sm">
                    {t("RDP 使用")}{view.os === "darwin"
                      ? t(" Windows App (App Store)。它不接受外部传入的密码, 连接时密码复制到剪贴板, 45 秒后清除。")
                      : t("系统远程桌面 (mstsc), 密码自动填入。")}
                  </p>
                  <Field
                    label={t("VNC 查看器")}
                    hint={
                      form.vncViewer === "external"
                        ? view.os === "darwin"
                          ? t("用系统屏幕共享打开, 密码复制到剪贴板, 45 秒后清除")
                          : t("TigerVNC 只能自动填 VNC 密码, 不支持 macOS 的用户名 + 密码认证, 画面也不能缩放")
                        : t("在标签页里打开, 自动填入用户名与密码, 画面随窗口缩放, 可弹出到独立窗口")
                    }
                  >
                    <Select value={form.vncViewer} onValueChange={(v) => set("vncViewer", v)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="builtin">{t("内置查看器")}</SelectItem>
                        <SelectItem value="external">{view.os === "darwin" ? t("系统屏幕共享") : t("外部程序 (TigerVNC 等)")}</SelectItem>
                      </SelectContent>
                    </Select>
                  </Field>
                  {view.os !== "darwin" && form.vncViewer === "external" && (
                    <Field label={t("VNC 查看器路径")} hint={t("留空时自动查找 TigerVNC; 其它查看器无法自动填密码, 会改用剪贴板")}>
                      <Input
                        value={form.vncViewerPath}
                        placeholder="C:\Program Files\TigerVNC\vncviewer.exe"
                        onChange={(e) => set("vncViewerPath", e.target.value)}
                      />
                    </Field>
                  )}
                  <div className="border-t pt-5">
                    <ToggleRow
                      label={t("显示服务器国旗")}
                      hint={
                        <>
                          {t("仅用于获取服务器国家信息, 不会收集 IP, 查询源为")}{" "}
                          <button
                            type="button"
                            className="text-primary underline-offset-2 hover:underline"
                            onClick={(e) => {
                              e.preventDefault();
                              openExternal("https://ip.czl.net");
                            }}
                          >
                            https://ip.czl.net
                          </button>
                        </>
                      }
                      checked={form.geoLookup}
                      onChange={(v) => set("geoLookup", v)}
                    />
                  </div>
                </>
              )}

              {tab === "vault" && (
                <>
                  <div className="bg-secondary space-y-1 rounded-md p-3 text-sm break-all">
                    <p>
                      {t("状态:")}<span className="font-medium">{status?.status ?? t("未知")}</span>
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
                            () => toast.success(t("已从服务器同步保险库")),
                            (e) => toast.error(errorText(e)),
                          )
                        }
                      >
                        <RefreshCwIcon /> {t("从服务器同步条目")}
                        </Button>
                    )}
                  </div>
                  <Field label={t("bw 路径")} hint={t("留空时在 PATH 中查找")}>
                    <Input value={form.bwPath} placeholder="/opt/homebrew/bin/bw" onChange={(e) => set("bwPath", e.target.value)} />
                  </Field>
                  <ToggleRow
                    label={t("记住解锁")}
                    hint={t("把 bw 会话密钥存进系统钥匙串, 重启 czlterm 不用再输主密码; 手动锁定或空闲超时锁定时会一并清除")}
                    checked={form.vaultRemember}
                    onChange={(v) => set("vaultRemember", v)}
                  />
                  <Field label={t("空闲自动锁定 (分钟)")} hint={t("0 表示直到退出程序")}>
                    <Input
                      inputMode="numeric"
                      value={form.vaultAutoLockMinutes}
                      onChange={(e) => set("vaultAutoLockMinutes", Number(e.target.value.replace(/\D/g, "")) || 0)}
                    />
                  </Field>
                </>
              )}

              {tab === "mcp" && (
                <>
                  <ToggleRow label={t("启用 MCP")} hint={t("在 127.0.0.1 上提供 MCP 端点, 以令牌鉴权; 只开这一项时 AI 只能列出连接")} checked={form.mcpEnabled} onChange={(v) => set("mcpEnabled", v)} />
                  <ToggleRow
                    label={t("允许 AI 访问服务器")}
                    hint={t("在全部 SSH 连接上列目录、读文件")}
                    checked={form.mcpAllowAccess}
                    disabled={!form.mcpEnabled}
                    onChange={(v) => set("mcpAllowAccess", v)}
                  />
                  <ToggleRow
                    label={t("允许 AI 写文件")}
                    checked={form.mcpAllowWrite}
                    disabled={!form.mcpEnabled || !form.mcpAllowAccess}
                    onChange={(v) => set("mcpAllowWrite", v)}
                  />
                  <ToggleRow
                    label={t("允许 AI 执行命令")}
                    hint={t("支持多行脚本")}
                    checked={form.mcpAllowExec}
                    disabled={!form.mcpEnabled || !form.mcpAllowAccess}
                    onChange={(v) => set("mcpAllowExec", v)}
                  />
                  {mcp && (
                    <>
                      <Snippet title={t("stdio 客户端 (Claude Desktop 等)")} text={mcp.stdioJson} onCopy={copy} />
                      {mcp.httpJson ? (
                        <Snippet title={t("HTTP 客户端 (Claude Code 等, 含令牌)")} text={mcp.httpJson} onCopy={copy} />
                      ) : (
                        <p className="text-muted-foreground text-xs">{t("启用并保存后显示 HTTP 配置。")}
                        </p>
                      )}
                      {mcp.running && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={() =>
                            api.RotateMCPToken().then(
                              (c) => {
                                setMcp(c);
                                toast.success(t("令牌已更换, 请更新 HTTP 客户端配置"));
                              },
                              (e) => toast.error(errorText(e)),
                            )
                          }
                        >
                          {t("更换令牌")}
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

      {!selfSaving.includes(tab) && (
        <div className="flex shrink-0 justify-end gap-2 border-t px-4 py-2">
          <Button variant="outline" onClick={onClose}>
            {t("取消")}
            </Button>
          <Button onClick={save} disabled={busy || !form}>
            {busy && <Loader2Icon className="animate-spin" />} {t("保存")}
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
  hint?: React.ReactNode;
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
  useLanguage();
  return (
    <div className="min-w-0 space-y-1">
      <div className="flex items-center justify-between">
        <Label className="text-muted-foreground text-xs">{title}</Label>
        <Button variant="ghost" size="icon-xs" aria-label={t("复制")} onClick={() => onCopy(text)}>
          <CopyIcon />
        </Button>
      </div>
      <pre className="bg-secondary rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap">{text}</pre>
    </div>
  );
}
