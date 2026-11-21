"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { CheckIcon, ChevronsUpDownIcon, CloudIcon, HardDriveIcon, LockIcon, UsersIcon } from "lucide-react";

import { spaceStatus, StatusDot } from "@/components/cloud/shared";
import { spaceLabel, useCloud } from "@/components/cloud/cloud-provider";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import type { SpaceView } from "@/lib/api";
import { cn } from "@/lib/utils";

export function SpaceIcon({ s, className }: { s: Pick<SpaceView, "kind">; className?: string }) {
  const Icon = s.kind === "local" ? HardDriveIcon : s.kind === "personal" ? CloudIcon : UsersIcon;
  return <Icon className={cn("size-4 shrink-0", className)} />;
}

/** 侧栏顶部的空间切换: 本机 / 我的云同步 / 各团队。列表、新建、脚本都跟着当前空间走。 */
export function SpaceSwitcher({ onManage }: { onManage: (tab: "account" | "teams") => void }) {
  useLanguage();
  const { spaces, current, user, switchSpace } = useCloud();
  const list = spaces?.spaces ?? [current];
  const st = current.kind === "local" ? null : spaceStatus(current);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button
          type="button"
          className="hover:bg-sidebar-accent/60 flex min-w-0 flex-1 items-center gap-2 rounded-md px-2 py-1 text-left"
          aria-label={t("切换空间")}
        >
          <SpaceIcon s={current} className="text-muted-foreground" />
          <span className="min-w-0 flex-1">
            <span className="flex items-center gap-1.5 truncate text-sm font-medium">
              <span className="truncate">{spaceLabel(current)}</span>
              {current.readOnly && <LockIcon className="text-muted-foreground size-3 shrink-0" />}
            </span>
            <span className="text-muted-foreground flex items-center gap-1.5 truncate text-xs">
              {st ? (
                <>
                  <StatusDot tone={st.tone} /> <span className="truncate">{st.text}</span>
                </>
              ) : (
                t("只在这台电脑上, 不同步")
              )}
            </span>
          </span>
          <ChevronsUpDownIcon className="text-muted-foreground size-3.5 shrink-0" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start" className="w-64">
        <DropdownMenuLabel className="text-muted-foreground text-xs font-normal">{t("空间")}</DropdownMenuLabel>
        {list.map((s) => {
          const ss = s.kind === "local" ? null : spaceStatus(s);
          return (
            <DropdownMenuItem key={s.key} onSelect={() => s.key !== current.key && void switchSpace(s.key)} className="gap-2">
              <SpaceIcon s={s} className="text-muted-foreground" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{spaceLabel(s)}</span>
                <span className="text-muted-foreground flex items-center gap-1.5 truncate text-xs">
                  {ss && ss.tone !== "ok" && ss.tone !== "idle" && <StatusDot tone={ss.tone} />}
                  {t("{0} 条连接", s.connections)}
                  {s.readOnly && ` · ${t("只读")}`}
                </span>
              </span>
              {s.key === current.key && <CheckIcon className="size-4" />}
            </DropdownMenuItem>
          );
        })}
        <DropdownMenuSeparator />
        {user ? (
          <DropdownMenuItem onSelect={() => onManage("teams")}>
            <UsersIcon /> {t("创建或管理团队…")}
          </DropdownMenuItem>
        ) : (
          <DropdownMenuItem onSelect={() => onManage("account")}>
            <CloudIcon /> {t("登录以开启云同步与团队…")}
          </DropdownMenuItem>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
