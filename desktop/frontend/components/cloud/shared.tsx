"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { Loader2Icon } from "lucide-react";

import { type SpaceView } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatAgo } from "@/lib/format";
import { cn } from "@/lib/utils";

/** 空间状态: 常态安静 (灰点), 异常显眼 (红 / 黄)。 */
export function spaceStatus(s: SpaceView): { tone: "ok" | "warn" | "error" | "idle"; text: string } {
  if (s.removed) return { tone: "warn", text: t("已不在这个团队, 本机副本只读") };
  if (s.need === "subscribe") return { tone: "warn", text: t("需要订阅, 现在只读") };
  if (s.need === "seats") return { tone: "warn", text: t("成员数超过席位, 现在只读") };
  if (s.status === "error") return { tone: "error", text: errorText(s.error) };
  if (s.status === "waiting_key") return { tone: "warn", text: t("等待团队管理员授予密钥") };
  if (s.pending > 0) return { tone: "idle", text: t("{0} 条修改待上传", s.pending) };
  if (s.syncedAt) return { tone: "ok", text: t("已同步 · {0}", formatAgo(s.syncedAt)) };
  return { tone: "idle", text: s.readOnly ? t("只读") : t("等待同步") };
}

export function StatusDot({ tone }: { tone: "ok" | "warn" | "error" | "idle" }) {
  return (
    <span
      className={cn(
        "inline-block size-1.5 shrink-0 rounded-full",
        tone === "ok" && "bg-success",
        tone === "warn" && "bg-warning",
        tone === "error" && "bg-destructive",
        tone === "idle" && "bg-muted-foreground/50",
      )}
    />
  );
}

export function roleLabel(role: string): string {
  return ({ owner: t("所有者"), admin: t("管理员"), member: t("成员"), viewer: t("查看者") } as Record<string, string>)[role] ?? role;
}

export function Section({
  icon: Icon,
  title,
  action,
  children,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <section className="space-y-3">
      <div className="flex items-center gap-2">
        <Icon className="text-muted-foreground size-4" />
        <h3 className="flex-1 text-sm font-semibold">{title}</h3>
        {action}
      </div>
      {children}
    </section>
  );
}

export function Notice({
  tone,
  icon: Icon,
  children,
}: {
  tone: "info" | "warning";
  icon: React.ComponentType<{ className?: string }>;
  children: React.ReactNode;
}) {
  return (
    <div
      className={cn(
        "flex items-center gap-3 rounded-lg border p-3 text-sm",
        tone === "warning" ? "border-warning/40 bg-warning/10" : "border-brand/30 bg-brand-soft",
      )}
    >
      <Icon className={cn("size-4 shrink-0", tone === "warning" ? "text-warning" : "text-brand-strong")} />
      {children}
    </div>
  );
}

export function Loading() {
  return (
    <div className="text-muted-foreground flex items-center justify-center py-16">
      <Loader2Icon className="animate-spin" />
    </div>
  );
}

/** 设置分区里跳到另一分区的一行入口: 图标 + 标题 + 状态说明, 整行可点。 */
export function LinkRow({
  icon: Icon,
  title,
  text,
  tone = "idle",
  onClick,
}: {
  icon: React.ComponentType<{ className?: string }>;
  title: string;
  text: string;
  tone?: "ok" | "warn" | "error" | "idle";
  onClick: () => void;
}) {
  useLanguage();
  return (
    <button type="button" onClick={onClick} className="hover:bg-accent/40 flex w-full items-center gap-3 px-3 py-2.5 text-left">
      <Icon className="text-muted-foreground size-4 shrink-0" />
      <span className="min-w-0 flex-1">
        <span className="block text-sm font-medium">{title}</span>
        <span className="text-muted-foreground flex items-center gap-1.5 truncate text-xs">
          {tone !== "idle" && <StatusDot tone={tone} />}
          <span className="truncate">{text}</span>
        </span>
      </span>
      <span className="text-muted-foreground text-xs">{t("管理")} ›</span>
    </button>
  );
}
