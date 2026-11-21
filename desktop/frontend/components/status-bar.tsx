"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useState } from "react";
import {
  CircleUserIcon,
  CloudIcon,
  FileCode2Icon,
  LockKeyholeIcon,
  LockKeyholeOpenIcon,
  LogInIcon,
  RefreshCwIcon,
  SettingsIcon,
  UsersIcon,
} from "lucide-react";
import { toast } from "sonner";

import { useCloud } from "@/components/cloud/cloud-provider";
import { UserAvatar } from "@/components/cloud/user-avatar";
import { useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip";
import { api } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

const vaultLabels: Record<string, string> = {
  checking: "保险库",
  unlocked: "保险库已解锁",
  locked: "保险库已锁定",
  unauthenticated: "bw 未登录",
  unavailable: "未找到 bw",
};

export type AccountTab = "account" | "sync" | "teams";

/**
 * 侧栏底部: 左边是账号 (头像、名字、云同步状态; 点开是账号菜单), 右边是保险库锁、脚本库、设置三个常用入口。
 * 未登录时显示「登录以同步」, 本机功能不受影响。
 */
export function StatusBar({
  onScripts,
  onSettings,
  onAccount,
}: {
  onScripts: () => void;
  onSettings: () => void;
  onAccount: (tab: AccountTab) => void;
}) {
  useLanguage();
  const { status, requestUnlock, lock } = useVault();
  const { account, user, cloud, refreshCloud } = useCloud();
  const [syncing, setSyncing] = useState(false);
  // 首次查询 bw 状态要一两秒, 期间显示中性文案, 不要先报"未找到 bw"。
  const state = status?.status ?? "checking";
  const unlocked = state === "unlocked";

  const cloudLine = !user
    ? account?.configured
      ? t("登录以同步与共享")
      : t("仅本机")
    : !cloud?.available
      ? t("同步服务暂不可用")
      : cloud.keyStatus === "not_enrolled"
        ? t("未开通云同步")
        : cloud.keyStatus === "locked"
          ? t("本机待解锁")
          : cloud.spaces.some((s) => s.status === "error")
            ? t("同步出错")
            : t("云同步已开启");
  const cloudBad = !!user && (!cloud?.available || cloud.keyStatus === "locked" || cloud.spaces.some((s) => s.status === "error"));

  async function syncNow() {
    setSyncing(true);
    // 保险库已解锁时顺带从服务器同步条目
    const [c, v] = await Promise.allSettled([api.CloudSyncNow(), unlocked ? api.VaultRefresh() : Promise.resolve()]);
    setSyncing(false);
    if (c.status === "rejected") toast.error(errorText(c.reason));
    else if (v.status === "rejected") toast.error(t("保险库同步失败: {0}", errorText(v.reason)));
    else toast.success(t("已同步"));
    void refreshCloud();
  }

  return (
    <div className="flex shrink-0 items-center gap-0.5 border-t px-1.5 py-1.5">
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <button
            type="button"
            className="hover:bg-sidebar-accent/60 flex min-w-0 flex-1 items-center gap-2 rounded-md px-1.5 py-1 text-left"
            aria-label={t("账号")}
          >
            <UserAvatar user={user} className="size-7 text-xs" />
            <span className="min-w-0 flex-1">
              <span className="block truncate text-sm font-medium">
                {user ? user.nickname || user.username || user.email : t("未登录")}
              </span>
              <span className={cn("block truncate text-xs", cloudBad ? "text-warning" : "text-muted-foreground")}>{cloudLine}</span>
            </span>
          </button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start" side="top" className="w-60">
          {user ? (
            <>
              <DropdownMenuLabel className="font-normal">
                <span className="block truncate text-sm font-medium">{user.nickname || user.username}</span>
                <span className="text-muted-foreground block truncate text-xs">{user.email}</span>
              </DropdownMenuLabel>
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={() => void syncNow()} disabled={syncing || cloud?.keyStatus !== "unlocked"}>
                <RefreshCwIcon className={cn(syncing && "animate-spin")} /> {t("立即同步")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onAccount("sync")}>
                <CloudIcon /> {t("云同步")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onAccount("teams")}>
                <UsersIcon /> {t("团队")}
              </DropdownMenuItem>
              <DropdownMenuItem onSelect={() => onAccount("account")}>
                <CircleUserIcon /> {t("账号")}
              </DropdownMenuItem>
            </>
          ) : (
            <DropdownMenuItem onSelect={() => onAccount("account")}>
              <LogInIcon /> {t("登录 CZL Connect")}
            </DropdownMenuItem>
          )}
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => (unlocked ? void lock() : void requestUnlock())}>
            {unlocked ? <LockKeyholeIcon /> : <LockKeyholeOpenIcon />} {unlocked ? t("锁定保险库") : t("解锁保险库")}
          </DropdownMenuItem>
          <DropdownMenuItem onSelect={onSettings}>
            <SettingsIcon /> {t("设置")}
          </DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>

      <Tooltip>
        <TooltipTrigger asChild>
          <Button
            variant="ghost"
            size="icon-sm"
            aria-label={t(vaultLabels[state] ?? state)}
            className={cn(!unlocked && "text-muted-foreground")}
            onClick={() => (unlocked ? void lock() : void requestUnlock())}
          >
            {unlocked ? <LockKeyholeOpenIcon /> : <LockKeyholeIcon />}
          </Button>
        </TooltipTrigger>
        <TooltipContent>
          {t(vaultLabels[state] ?? state)}
          {unlocked ? ` · ${t("点击锁定")}` : state === "locked" ? ` · ${t("点击解锁")}` : ""}
        </TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={t("脚本")} onClick={onScripts}>
            <FileCode2Icon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t("常用脚本")}</TooltipContent>
      </Tooltip>
      <Tooltip>
        <TooltipTrigger asChild>
          <Button variant="ghost" size="icon-sm" aria-label={t("设置")} onClick={onSettings}>
            <SettingsIcon />
          </Button>
        </TooltipTrigger>
        <TooltipContent>{t("设置")}</TooltipContent>
      </Tooltip>
    </div>
  );
}
