"use client";

import "@xterm/xterm/css/xterm.css";

import { useEffect, useRef, useState } from "react";
import { ChevronDownIcon, ChevronUpIcon, XIcon } from "lucide-react";
import type { Terminal } from "@xterm/xterm";
import type { SearchAddon } from "@xterm/addon-search";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { api, openExternal } from "@/lib/api";
import { t } from "@/lib/i18n";
import { ClipboardGetText, ClipboardSetText } from "@/wailsjs/runtime/runtime";

/** 终端统一暗色, 不跟随应用主题。 */
export const TERMINAL_BACKGROUND = "#0f1115";
const theme = {
  background: TERMINAL_BACKGROUND,
  foreground: "#d4d7dd",
  cursor: "#e6e6e6",
  cursorAccent: TERMINAL_BACKGROUND,
  selectionBackground: "#3a4150",
  black: "#1c1f26",
  red: "#e06c75",
  green: "#98c379",
  yellow: "#e5c07b",
  blue: "#61afef",
  magenta: "#c678dd",
  cyan: "#56b6c2",
  white: "#c8ccd4",
  brightBlack: "#5c6370",
  brightRed: "#ef7f88",
  brightGreen: "#a9d48a",
  brightYellow: "#f0cf8c",
  brightBlue: "#74bdf7",
  brightMagenta: "#d48ae6",
  brightCyan: "#67c7d3",
  brightWhite: "#f0f2f5",
};

const isMac = typeof navigator !== "undefined" && /Mac/i.test(navigator.platform);

let endpoint: Promise<{ url: string; token: string }> | null = null;
/** 终端 WebSocket 地址与令牌在本次运行内不变, 取一次缓存起来。 */
function terminalEndpoint() {
  endpoint ??= api.GetTerminalEndpoint().catch((e) => {
    endpoint = null;
    throw e;
  });
  return endpoint;
}

/**
 * 一个内置终端会话的显示。会话归后端, 这里只是观看者: 连上时后端先回放最近输出再转发实时输出,
 * 断开 (非命令退出) 后自动重连。命令退出后提示按 Enter 重新连接。
 */
export function TerminalView({
  sessionId,
  active,
  onExit,
  onReconnect,
}: {
  sessionId: string;
  active: boolean;
  onExit: (code: number) => void;
  onReconnect: () => void;
}) {
  const box = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const searchRef = useRef<SearchAddon | null>(null);
  const fitRef = useRef<(() => void) | null>(null);
  const exitedRef = useRef(false);
  // 回调随父组件渲染变化, 用 ref 取最新的, 不让终端随之重建。
  const handlers = useRef({ onExit, onReconnect });
  useEffect(() => {
    handlers.current = { onExit, onReconnect };
  });
  const [searchOpen, setSearchOpen] = useState(false);
  const [query, setQuery] = useState("");

  useEffect(() => {
    const el = box.current;
    if (!el) return;
    let disposed = false;
    let ws: WebSocket | null = null;
    let retry: ReturnType<typeof setTimeout> | undefined;
    let failures = 0;
    const cleanups: (() => void)[] = [];
    exitedRef.current = false;

    void (async () => {
      const [{ Terminal }, { FitAddon }, { SearchAddon }, { WebLinksAddon }] = await Promise.all([
        import("@xterm/xterm"),
        import("@xterm/addon-fit"),
        import("@xterm/addon-search"),
        import("@xterm/addon-web-links"),
      ]);
      if (disposed) return;
      const term = new Terminal({
        theme,
        fontFamily: "'Cascadia Mono', 'JetBrains Mono', Menlo, Consolas, 'Courier New', monospace",
        fontSize: 13,
        lineHeight: 1.15,
        cursorBlink: true,
        scrollback: 10000,
        macOptionIsMeta: true,
        allowProposedApi: true,
      });
      const fit = new FitAddon();
      const search = new SearchAddon();
      term.loadAddon(fit);
      term.loadAddon(search);
      term.loadAddon(new WebLinksAddon((_e, uri) => openExternal(uri)));
      term.open(el);
      termRef.current = term;
      searchRef.current = search;

      const encoder = new TextEncoder();
      const send = (data: string) => {
        if (exitedRef.current) {
          if (data === "\r") handlers.current.onReconnect();
          return;
        }
        if (ws?.readyState === WebSocket.OPEN) ws.send(encoder.encode(data));
      };
      const sendSize = () => {
        if (ws?.readyState === WebSocket.OPEN) ws.send(JSON.stringify({ type: "resize", cols: term.cols, rows: term.rows }));
      };
      const refit = () => {
        // 隐藏的标签量不出尺寸, 切回来时再算。
        if (el.offsetWidth === 0 || el.offsetHeight === 0) return;
        fit.fit();
      };
      fitRef.current = refit;
      refit();

      const copySelection = () => {
        const text = term.getSelection();
        if (text) void ClipboardSetText(text);
        return !!text;
      };
      const paste = () => void ClipboardGetText().then((text) => text && term.paste(text));
      term.attachCustomKeyEventHandler((e) => {
        if (e.type !== "keydown") return true;
        const mod = isMac ? e.metaKey : e.ctrlKey && e.shiftKey;
        if (!mod) return true;
        const key = e.key.toLowerCase();
        if (key === "c") {
          copySelection();
          e.preventDefault();
          return false;
        }
        if (key === "v") {
          paste();
          e.preventDefault();
          return false;
        }
        if (key === "f") {
          setSearchOpen(true);
          e.preventDefault();
          return false;
        }
        return true;
      });
      // 右键: 有选中内容时复制, 否则粘贴, 与 Windows Terminal 一致。
      const onContextMenu = (e: MouseEvent) => {
        e.preventDefault();
        if (!copySelection()) paste();
        else term.clearSelection();
      };
      el.addEventListener("contextmenu", onContextMenu);
      cleanups.push(() => el.removeEventListener("contextmenu", onContextMenu));

      const onData = term.onData(send);
      const onBinary = term.onBinary((data) => {
        if (ws?.readyState === WebSocket.OPEN) ws.send(Uint8Array.from(data, (c) => c.charCodeAt(0)));
      });
      const onResize = term.onResize(sendSize);
      cleanups.push(() => {
        onData.dispose();
        onBinary.dispose();
        onResize.dispose();
      });
      const observer = new ResizeObserver(() => refit());
      observer.observe(el);
      cleanups.push(() => observer.disconnect());

      const connect = async () => {
        let ep: { url: string; token: string };
        try {
          ep = await terminalEndpoint();
        } catch {
          scheduleRetry();
          return;
        }
        if (disposed) return;
        const sock = new WebSocket(`${ep.url}?id=${encodeURIComponent(sessionId)}&token=${encodeURIComponent(ep.token)}`);
        sock.binaryType = "arraybuffer";
        let opened = false;
        sock.onopen = () => {
          opened = true;
          failures = 0;
          // 连上时后端先回放最近输出, 清屏避免重连后内容重复。
          term.reset();
          sendSize();
        };
        sock.onmessage = (ev) => {
          if (ev.data instanceof ArrayBuffer) {
            term.write(new Uint8Array(ev.data));
            return;
          }
          try {
            const msg = JSON.parse(ev.data as string) as { type: string; code: number };
            if (msg.type === "exit") {
              exitedRef.current = true;
              term.write(`\r\n\x1b[33m[czlterm] ${t("连接已结束 (退出码 {0}), 按 Enter 重新连接", msg.code)}\x1b[0m\r\n`);
              handlers.current.onExit(msg.code);
            }
          } catch {
            /* 忽略无法识别的控制消息 */
          }
        };
        sock.onclose = () => {
          if (ws === sock) ws = null;
          if (disposed || exitedRef.current) return;
          if (!opened) failures++;
          scheduleRetry();
        };
        ws = sock;
      };
      const scheduleRetry = () => {
        if (disposed) return;
        if (failures >= 5) {
          term.write(`\r\n\x1b[31m[czlterm] ${t("无法连接到终端会话")}\x1b[0m\r\n`);
          return;
        }
        retry = setTimeout(() => void connect(), 500 * Math.max(1, failures));
      };
      void connect();
    })();

    return () => {
      disposed = true;
      clearTimeout(retry);
      ws?.close();
      cleanups.forEach((fn) => fn());
      termRef.current?.dispose();
      termRef.current = null;
      searchRef.current = null;
      fitRef.current = null;
    };
  }, [sessionId]);

  // 切回标签时重新量尺寸并聚焦。
  useEffect(() => {
    if (!active) return;
    const id = requestAnimationFrame(() => {
      fitRef.current?.();
      termRef.current?.focus();
    });
    return () => cancelAnimationFrame(id);
  }, [active]);

  function find(backward = false) {
    const s = searchRef.current;
    if (!s || !query) return;
    if (backward) s.findPrevious(query);
    else s.findNext(query);
  }

  function closeSearch() {
    setSearchOpen(false);
    searchRef.current?.clearDecorations();
    termRef.current?.focus();
  }

  return (
    <div className="relative h-full w-full px-2 pt-1.5" style={{ background: TERMINAL_BACKGROUND }}>
      <div ref={box} className="h-full w-full" />
      {searchOpen && (
        <div className="bg-popover text-popover-foreground absolute top-2 right-4 flex items-center gap-1 rounded-md border p-1 shadow-md">
          <Input
            autoFocus
            className="h-7 w-48 text-sm"
            placeholder={t("搜索")}
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") find(e.shiftKey);
              if (e.key === "Escape") closeSearch();
            }}
          />
          <Button variant="ghost" size="icon-sm" aria-label={t("上一个")} onClick={() => find(true)}>
            <ChevronUpIcon />
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={t("下一个")} onClick={() => find()}>
            <ChevronDownIcon />
          </Button>
          <Button variant="ghost" size="icon-sm" aria-label={t("关闭")} onClick={closeSearch}>
            <XIcon />
          </Button>
        </div>
      )}
    </div>
  );
}
