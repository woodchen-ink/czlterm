"use client";

import { useSyncExternalStore } from "react";

import { AppShell } from "@/components/app-shell";
import { VNCWindowPage } from "@/components/vnc-window";
import { isVNCWindow } from "@/lib/api";

const noop = () => () => {};

/**
 * 按所在窗口选页面: 主窗口是连接管理, 弹出的独立 VNC 窗口只有画面。
 * 静态导出时不知道是哪种窗口, 先渲染空, 水合后再选, 免得独立窗口里先挂上 AppShell 去调没有绑定的接口。
 */
export function WindowRoot() {
  const mode = useSyncExternalStore(
    noop,
    () => (isVNCWindow() ? "vnc" : "main"),
    () => null,
  );
  if (mode === null) return null;
  return mode === "vnc" ? <VNCWindowPage /> : <AppShell />;
}
