import type { Metadata } from "next";
import { Geist, Geist_Mono } from "next/font/google";

import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";

import "./globals.css";

// next/font 在构建期下载并自托管字体, 导出产物里是本地文件, 运行时不联网。
const geistSans = Geist({ variable: "--font-geist-sans", subsets: ["latin"] });
const geistMono = Geist_Mono({ variable: "--font-geist-mono", subsets: ["latin"] });

export const metadata: Metadata = {
  title: "czlterm",
  description: "SSH / RDP / VNC 连接管理",
};

// 赶在首帧前跟随系统深浅色并标记 macOS, 避免闪白与红绿灯压住内容。
const bootScript = `(function(){var d=document.documentElement;var m=window.matchMedia("(prefers-color-scheme: dark)");function a(){d.classList.toggle("dark",m.matches);d.style.colorScheme=m.matches?"dark":"light"}a();m.addEventListener("change",a);if(/Mac/.test(navigator.platform))d.classList.add("mac")})()`;

export default function RootLayout({ children }: { children: React.ReactNode }) {
  return (
    <html lang="zh-CN" className={`${geistSans.variable} ${geistMono.variable} h-full antialiased`} suppressHydrationWarning>
      <head>
        <script dangerouslySetInnerHTML={{ __html: bootScript }} />
      </head>
      {/* 主壳锁死视口高度: 侧栏与右侧面板各自内部滚动。 */}
      <body className="h-full overflow-hidden">
        <TooltipProvider delayDuration={300}>
          <div className="flex h-full flex-col">
            {/* 仅 macOS 显示: 给红绿灯让位, 同时作为拖动窗口的把手。 */}
            <div
              className="mac-titlebar bg-sidebar border-border text-muted-foreground h-9 shrink-0 items-center justify-center border-b text-xs font-medium select-none"
              style={{ "--wails-draggable": "drag" } as React.CSSProperties}
            >
              czlterm
            </div>
            <div className="min-h-0 flex-1">{children}</div>
          </div>
          <Toaster position="bottom-right" />
        </TooltipProvider>
      </body>
    </html>
  );
}
