"use client";

import { useEffect, useState } from "react";
import { BugIcon, DownloadIcon, Loader2Icon, MessageCircleIcon, RefreshCwIcon, StarIcon, TagIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { api, Events, onEvent, openExternal, type AboutInfo, type UpdateInfo } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes } from "@/lib/format";

/** 关于: 版本与在线更新, 以及开源项目的推广入口。 */
export function AboutPanel() {
  const [about, setAbout] = useState<AboutInfo | null>(null);
  const [info, setInfo] = useState<UpdateInfo | null>(null);
  const [checking, setChecking] = useState(false);
  const [installing, setInstalling] = useState(false);
  const [progress, setProgress] = useState<{ done: number; total: number } | null>(null);

  useEffect(() => {
    api.About().then(setAbout).catch(() => {});
    api
      .GetPendingUpdate()
      .then((p) => p && setInfo(p))
      .catch(() => {});
    return onEvent<{ done: number; total: number }>(Events.updateProgress, setProgress);
  }, []);

  async function check() {
    setChecking(true);
    try {
      const r = await api.CheckForUpdate();
      setInfo(r);
      if (!r.available) toast.info(r.message || "已是最新版本");
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setChecking(false);
    }
  }

  async function install() {
    setInstalling(true);
    try {
      await api.InstallUpdate();
      toast.success("正在安装, 完成后自动重新打开");
    } catch (e) {
      toast.error(errorText(e));
      setInstalling(false);
      setProgress(null);
    }
  }

  if (!about) return null;
  const rel = info?.available ? info.release : null;

  return (
    <div className="space-y-5">
      <div className="flex items-center justify-between gap-4">
        <div className="flex items-center gap-3">
          {/* 静态导出下没有图片优化服务, 直接用 img。 */}
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src="/logo.png" alt="" className="size-12" />
          <div>
            <p className="text-base font-semibold">czlterm</p>
            <p className="text-muted-foreground text-xs">版本 {about.version}</p>
          </div>
        </div>
        {rel ? (
          <Button onClick={install} disabled={installing}>
            {installing ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
            {installing && progress?.total ? `下载中 ${Math.round((progress.done / progress.total) * 100)}%` : `更新到 ${rel.version}`}
          </Button>
        ) : (
          <Button variant="outline" onClick={check} disabled={checking}>
            {checking ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} 检查更新
          </Button>
        )}
      </div>

      {rel && (
        <div className="bg-secondary space-y-1 rounded-md p-3 text-sm">
          <p className="font-medium">
            {rel.version} 已发布{rel.assetSize ? ` · ${formatBytes(rel.assetSize)}` : ""}
          </p>
          {rel.notes && <p className="text-muted-foreground czl-scroll max-h-40 overflow-y-auto text-xs whitespace-pre-wrap">{rel.notes}</p>}
          <p className="text-muted-foreground text-xs">安装包下载后先校验 SHA-256, 再替换安装并重新打开。</p>
        </div>
      )}

      <div className="space-y-3 rounded-md border p-4">
        <p className="text-sm font-medium">czlterm 是开源项目</p>
        <p className="text-muted-foreground text-sm">
          轻量的 SSH / RDP / VNC 连接管理, 凭据直接取自 Vaultwarden, 内置 MCP。代码完全公开, 欢迎使用、反馈与贡献。
          如果它帮到了你, 去 GitHub 点个 Star, 或者推荐给身边同样在管服务器的朋友, 这是对项目最大的支持。
        </p>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" onClick={() => openExternal(about.projectUrl)}>
            <StarIcon /> 在 GitHub 上 Star
          </Button>
          {about.forumUrl && (
            <Button size="sm" variant="outline" onClick={() => openExternal(about.forumUrl)}>
              <MessageCircleIcon /> 论坛讨论帖
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => openExternal(about.issuesUrl)}>
            <BugIcon /> 反馈问题
          </Button>
          <Button size="sm" variant="ghost" onClick={() => openExternal(about.releasesUrl)}>
            <TagIcon /> 更新日志
          </Button>
        </div>
      </div>
    </div>
  );
}
