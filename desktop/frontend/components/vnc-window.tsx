"use client";

import { useEffect, useState } from "react";
import { MaximizeIcon, MinimizeIcon } from "lucide-react";

import { TERMINAL_BACKGROUND } from "@/components/terminal-view";
import { ToolButton, VNCView, WithTip } from "@/components/vnc-view";
import { vncWindow, type VNCWindowParams } from "@/lib/api";
import { setLanguage, t, type Locale } from "@/lib/i18n";
import { WindowFullscreen, WindowIsFullscreen, WindowUnfullscreen } from "@/wailsjs/runtime/runtime";

/** 弹出的独立 VNC 窗口: 整个窗口只显示一个会话, 画面经主程序的本机 WebSocket 转发。 */
export function VNCWindowPage() {
  const [params, setParams] = useState<VNCWindowParams | null>(null);
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    void vncWindow.Params().then((p) => {
      setLanguage(p.language as Locale, false);
      document.title = p.session.title;
      setParams(p);
    });
  }, []);

  async function toggleFullscreen() {
    const now = await WindowIsFullscreen();
    if (now) WindowUnfullscreen();
    else WindowFullscreen();
    setFullscreen(!now);
  }

  if (!params) return <div className="h-full" style={{ background: TERMINAL_BACKGROUND }} />;
  const label = fullscreen ? t("退出全屏") : t("全屏");
  return (
    <VNCView
      sessionId={params.session.id}
      title={params.session.title}
      active
      endpoint={() => Promise.resolve({ url: params.endpoint, token: params.token })}
      credentials={() => Promise.resolve(params.session)}
      actions={
        <WithTip label={label}>
          <ToolButton label={label} onClick={() => void toggleFullscreen()}>
            {fullscreen ? <MinimizeIcon /> : <MaximizeIcon />}
          </ToolButton>
        </WithTip>
      }
    />
  );
}
