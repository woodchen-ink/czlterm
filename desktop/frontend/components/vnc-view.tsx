"use client";

import { useEffect, useRef, useState } from "react";
import { ChevronDownIcon, ClipboardPasteIcon, KeyboardIcon, Loader2Icon, RotateCwIcon } from "lucide-react";
import type RFB from "@novnc/novnc";

import { TERMINAL_BACKGROUND } from "@/components/terminal-view";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { LocalEndpoint } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { t } from "@/lib/i18n";
import { cn } from "@/lib/utils";
import { ClipboardGetText, ClipboardSetText } from "@/wailsjs/runtime/runtime";

/** 与后端 vnc.CloseDialFailed 一致: 拨号失败时 WebSocket 的关闭码, 原因文本以错误码开头。 */
const CLOSE_DIAL_FAILED = 4000;

/**
 * 画面显示方式。fit 在本地缩放到窗口大小; remote 请求服务器把分辨率改成窗口大小,
 * 服务器不支持 (如 macOS) 时退回本地缩放; original 按原始像素显示, 超出部分滚动。
 */
type ScaleMode = "fit" | "remote" | "original";

export interface VNCCredentials {
  username: string;
  password: string;
}

type Status =
  | { state: "connecting" }
  /** 连接没配凭据 (连接时手动输入), 服务器要用户名 / 密码。 */
  | { state: "credentials"; username: boolean; initialUsername: string }
  | { state: "connected" }
  | { state: "disconnected"; error: string };

/**
 * 一个内置 VNC 会话的画面。noVNC 在页面里完成 RFB 协议与认证, 字节经本机 WebSocket 由后端转发到服务器。
 * 断开后不自动重连 (认证失败时重连只会反复失败), 由用户点重新连接。
 */
export function VNCView({
  sessionId,
  title,
  active,
  endpoint,
  credentials,
  onConnectedChange,
  onCredentials,
  actions,
}: {
  sessionId: string;
  title: string;
  active: boolean;
  endpoint: () => Promise<LocalEndpoint>;
  credentials: () => Promise<VNCCredentials>;
  onConnectedChange?: (connected: boolean) => void;
  /** 手动输入的凭据认证成功后调用, 主窗口据此记进会话, 弹出的独立窗口不必再输。 */
  onCredentials?: (credentials: VNCCredentials) => void;
  /** 工具栏末尾的按钮, 主窗口放「弹出」, 独立窗口放「全屏」。 */
  actions?: React.ReactNode;
}) {
  const screen = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<Status>({ state: "connecting" });
  const [desktop, setDesktop] = useState("");
  const [scale, setScale] = useState<ScaleMode>("fit");
  const [attempt, setAttempt] = useState(0);
  // 回调与选项随父组件渲染变化, 用 ref 取最新的, 不让连接随之重建。
  const latest = useRef({ endpoint, credentials, onConnectedChange, onCredentials, scale });
  useEffect(() => {
    latest.current = { endpoint, credentials, onConnectedChange, onCredentials, scale };
  });
  // 手动输入的凭据: 提交后先放 pending, 认证成功才转正, 重新连接时直接用, 输错的不会留下来反复失败。
  const manual = useRef<VNCCredentials | null>(null);
  const pending = useRef<VNCCredentials | null>(null);

  useEffect(() => {
    const el = screen.current;
    if (!el) return;
    let disposed = false;
    let rfb: RFB | null = null;
    queueMicrotask(() => !disposed && setStatus({ state: "connecting" }));

    const fail = (error: string) => {
      if (disposed) return;
      setStatus({ state: "disconnected", error });
      latest.current.onConnectedChange?.(false);
    };

    void (async () => {
      let RFBClass: typeof RFB;
      let ep: LocalEndpoint;
      let creds: VNCCredentials;
      try {
        [{ default: RFBClass }, ep, creds] = await Promise.all([
          import("@novnc/novnc"),
          latest.current.endpoint(),
          manual.current ? Promise.resolve(manual.current) : latest.current.credentials(),
        ]);
      } catch (e) {
        fail(errorText(e));
        return;
      }
      if (disposed) return;

      // 自己建 WebSocket 交给 noVNC, 以便读到后端拨号失败时的关闭原因; noVNC 只报告断开, 不带原因。
      const ws = new WebSocket(`${ep.url}/vnc?id=${encodeURIComponent(sessionId)}&token=${encodeURIComponent(ep.token)}`);
      ws.binaryType = "arraybuffer";
      let closeReason = "";
      ws.addEventListener("close", (e) => {
        if (e.code === CLOSE_DIAL_FAILED && e.reason) closeReason = errorText(e.reason);
      });

      // 用户名与密码一开始就给: macOS 屏幕共享用用户名 + 密码认证, 普通 VNC 只用密码。
      const credentialsGiven = { username: creds.username || undefined, password: creds.password || undefined };
      rfb = new RFBClass(el, ws, { shared: true, credentials: credentialsGiven });
      rfbRef.current = rfb;
      rfb.background = TERMINAL_BACKGROUND;
      applyScale(rfb, latest.current.scale);

      let authError = "";
      rfb.addEventListener("connect", () => {
        if (disposed) return;
        if (pending.current) {
          manual.current = pending.current;
          latest.current.onCredentials?.(pending.current);
          pending.current = null;
        }
        setStatus({ state: "connected" });
        latest.current.onConnectedChange?.(true);
        rfb?.focus();
      });
      rfb.addEventListener("disconnect", () => {
        if (rfbRef.current === rfb) rfbRef.current = null;
        pending.current = null;
        // close 事件早于 noVNC 的 disconnect 派发, 这时关闭原因已经读到。
        fail(authError || closeReason || t("连接已断开"));
      });
      rfb.addEventListener("credentialsrequired", (e) => {
        // 一开始已经给过凭据, 还要就是缺了服务器要求的那一项: 连接选了「连接时手动输入」时在画面上输入。
        const types = (e as CustomEvent<{ types: string[] }>).detail.types;
        if (!disposed) {
          setStatus({ state: "credentials", username: types.includes("username"), initialUsername: creds.username });
        }
      });
      rfb.addEventListener("securityfailure", (e) => {
        const { status, reason } = (e as CustomEvent<{ status: number; reason?: string }>).detail;
        authError = t("认证失败: {0}", reason || t("用户名或密码不正确") + ` (${status})`);
      });
      rfb.addEventListener("desktopname", (e) => {
        if (!disposed) setDesktop((e as CustomEvent<{ name: string }>).detail.name);
      });
      rfb.addEventListener("clipboard", (e) => {
        void ClipboardSetText((e as CustomEvent<{ text: string }>).detail.text);
      });
    })();

    return () => {
      disposed = true;
      if (rfbRef.current === rfb) rfbRef.current = null;
      rfb?.disconnect();
    };
  }, [sessionId, attempt]);

  useEffect(() => {
    if (rfbRef.current) applyScale(rfbRef.current, scale);
  }, [scale]);

  // 切回标签时把键盘焦点交给画面。
  useEffect(() => {
    if (active && status.state === "connected") rfbRef.current?.focus();
  }, [active, status.state]);

  function submitCredentials(username: string, password: string) {
    const rfb = rfbRef.current;
    if (!rfb) return;
    setStatus({ state: "connecting" });
    pending.current = { username, password };
    rfb.sendCredentials({ username, password });
  }

  async function pasteClipboard() {
    const text = await ClipboardGetText();
    if (text) rfbRef.current?.clipboardPasteFrom(text);
    rfbRef.current?.focus();
  }

  const connected = status.state === "connected";
  const scaleLabels: Record<ScaleMode, string> = {
    fit: t("适应窗口"),
    remote: t("调整远程分辨率"),
    original: t("原始大小"),
  };

  return (
    <div className="flex h-full w-full flex-col" style={{ background: TERMINAL_BACKGROUND }}>
      <div className="flex h-8 shrink-0 items-center gap-1 border-b border-white/10 bg-[#161a20] pr-1 pl-3 text-xs text-[#c8ccd4]">
        <span
          className={cn("size-1.5 shrink-0 rounded-full", connected ? "bg-emerald-500" : "bg-white/30")}
          title={connected ? t("已连接") : t("已断开")}
        />
        <span className="min-w-0 flex-1 truncate" title={desktop || title}>
          {desktop || title}
        </span>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <ToolButton label={t("画面大小")} className="w-auto gap-0.5 px-1.5 text-xs [&_svg]:size-3">
              {scaleLabels[scale]}
              <ChevronDownIcon />
            </ToolButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuRadioGroup value={scale} onValueChange={(v) => setScale(v as ScaleMode)}>
              {(Object.keys(scaleLabels) as ScaleMode[]).map((mode) => (
                <DropdownMenuRadioItem key={mode} value={mode}>
                  {scaleLabels[mode]}
                </DropdownMenuRadioItem>
              ))}
            </DropdownMenuRadioGroup>
          </DropdownMenuContent>
        </DropdownMenu>
        <WithTip label={t("把本机剪贴板粘贴到远程")}>
          <ToolButton label={t("把本机剪贴板粘贴到远程")} disabled={!connected} onClick={() => void pasteClipboard()}>
            <ClipboardPasteIcon />
          </ToolButton>
        </WithTip>
        <WithTip label={t("发送 Ctrl+Alt+Del")}>
          <ToolButton label={t("发送 Ctrl+Alt+Del")} disabled={!connected} onClick={() => rfbRef.current?.sendCtrlAltDel()}>
            <KeyboardIcon />
          </ToolButton>
        </WithTip>
        {actions}
      </div>
      <div className="relative min-h-0 flex-1">
        <div ref={screen} className="absolute inset-0" />
        {status.state !== "connected" && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-3 text-sm text-[#c8ccd4]">
            {status.state === "connecting" ? (
              <>
                <Loader2Icon className="size-5 animate-spin" />
                <span>{t("正在连接 {0}", title)}</span>
              </>
            ) : status.state === "credentials" ? (
              <CredentialsForm
                needUsername={status.username}
                initialUsername={status.initialUsername}
                onSubmit={submitCredentials}
                onCancel={() => rfbRef.current?.disconnect()}
              />
            ) : (
              <>
                <span className="max-w-md px-6 text-center">{status.error}</span>
                <Button size="sm" variant="secondary" onClick={() => setAttempt((n) => n + 1)}>
                  <RotateCwIcon />
                  {t("重新连接")}
                </Button>
              </>
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** 在画面上输入 VNC 凭据。macOS 屏幕共享要用户名 + 登录密码, 普通 VNC 只要 VNC 密码。 */
function CredentialsForm({
  needUsername,
  initialUsername,
  onSubmit,
  onCancel,
}: {
  needUsername: boolean;
  initialUsername: string;
  onSubmit: (username: string, password: string) => void;
  onCancel: () => void;
}) {
  const [username, setUsername] = useState(initialUsername);
  const [password, setPassword] = useState("");
  const field =
    "h-8 w-full rounded-md border border-white/15 bg-black/30 px-2.5 text-sm text-white outline-none placeholder:text-white/40 focus:border-white/40";
  return (
    <form
      className="flex w-72 flex-col gap-2.5 rounded-lg border border-white/10 bg-[#161a20] p-4"
      onSubmit={(e) => {
        e.preventDefault();
        onSubmit(username, password);
      }}
    >
      <p className="text-[#c8ccd4]">{needUsername ? t("输入用户名与密码") : t("输入 VNC 密码")}</p>
      {needUsername && (
        <input
          className={field}
          placeholder={t("用户名")}
          autoFocus={!initialUsername}
          value={username}
          onChange={(e) => setUsername(e.target.value)}
        />
      )}
      <input
        className={field}
        type="password"
        placeholder={t("密码")}
        autoFocus={!needUsername || !!initialUsername}
        value={password}
        onChange={(e) => setPassword(e.target.value)}
      />
      <div className="flex justify-end gap-2 pt-1">
        <Button type="button" size="sm" variant="ghost" className="text-[#c8ccd4] hover:bg-white/10 hover:text-white" onClick={onCancel}>
          {t("取消")}
        </Button>
        <Button type="submit" size="sm" variant="secondary" disabled={!password || (needUsername && !username)}>
          {t("连接")}
        </Button>
      </div>
    </form>
  );
}

function applyScale(rfb: RFB, mode: ScaleMode) {
  rfb.scaleViewport = mode !== "original";
  rfb.resizeSession = mode === "remote";
}

/** 暗色工具栏上的图标按钮, 不跟随应用主题。 */
export function ToolButton({
  label,
  children,
  className,
  ...props
}: React.ComponentProps<"button"> & { label: string }) {
  return (
    <button
      type="button"
      aria-label={label}
      className={cn(
        "flex size-6 items-center justify-center rounded-md text-[#aab0bb] outline-none hover:bg-white/10 hover:text-white focus-visible:ring-2 focus-visible:ring-white/30 disabled:pointer-events-none disabled:opacity-40 [&_svg]:size-3.5",
        className,
      )}
      {...props}
    >
      {children}
    </button>
  );
}

export function WithTip({ label, children }: { label: string; children: React.ReactElement }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>{children}</TooltipTrigger>
      <TooltipContent>{label}</TooltipContent>
    </Tooltip>
  );
}
