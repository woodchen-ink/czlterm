"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { FileCode2Icon, LockKeyholeIcon, LockKeyholeOpenIcon, RefreshCwIcon, SettingsIcon } from "lucide-react";

import { useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import type { SyncStatus } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

const vaultLabels: Record<string, string> = {
  checking: "保险库",
  unlocked: "保险库已解锁",
  locked: "保险库已锁定",
  unauthenticated: "bw 未登录",
  unavailable: "未找到 bw",
};

/** 侧栏底部: 保险库状态、同步、脚本库、设置。 */
export function StatusBar({
  sync,
  syncEnabled,
  onSync,
  onScripts,
  onSettings,
}: {
  sync: SyncStatus | null;
  syncEnabled: boolean;
  onSync: () => void;
  onScripts: () => void;
  onSettings: () => void;
}) {
  useLanguage();
  const { status, requestUnlock, lock } = useVault();
  // 首次查询 bw 状态要一两秒, 期间显示中性文案, 不要先报"未找到 bw"。
  const state = status?.status ?? "checking";
  const unlocked = state === "unlocked";

  const syncHint = sync?.error
    ? t("同步失败: {0}", errorText(sync.error))
    : sync?.lastSync
      ? t("上次同步 {0}", formatTime(sync.lastSync))
      : syncEnabled
        ? t("同步到 git 仓库")
        : t("未配置同步仓库, 点击仅做本地提交");

  return (
    <div className="flex shrink-0 items-center gap-1 border-t px-2 py-1.5">
      <Button
        variant="ghost"
        size="sm"
        className={cn("min-w-0 flex-1 justify-start", !unlocked && "text-muted-foreground")}
        onClick={() => (unlocked ? void lock() : void requestUnlock())}
      >
        {unlocked ? <LockKeyholeOpenIcon /> : <LockKeyholeIcon />}
        <span className="truncate">{t(vaultLabels[state] ?? state)}</span>
      </Button>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={t("同步")} onClick={onSync} disabled={sync?.running}>
            <RefreshCwIcon className={cn(sync?.running && "animate-spin", sync?.error && "text-destructive")} />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{syncHint}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={t("脚本")} onClick={onScripts}>
            <FileCode2Icon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t("常用脚本")}</TooltipContent>
      </Tooltip>
      <Button variant="ghost" size="icon-sm" aria-label={t("设置")} onClick={onSettings}>
        <SettingsIcon />
      </Button>
    </div>
  );
}
