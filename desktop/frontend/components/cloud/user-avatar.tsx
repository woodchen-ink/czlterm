"use client";

import { useState } from "react";
import { UserIcon } from "lucide-react";

import type { AccountUser } from "@/lib/api";
import { cn } from "@/lib/utils";

/** 账号头像: 有图用图, 图挂了或没有时用名字首字; 未登录显示人形图标。 */
export function UserAvatar({ user, className }: { user: AccountUser | null; className?: string }) {
  const [broken, setBroken] = useState(false);
  const name = user?.nickname || user?.username || user?.email || "";
  const base = cn("bg-secondary text-secondary-foreground flex shrink-0 items-center justify-center overflow-hidden rounded-full text-sm font-medium", className);
  if (!user) {
    return (
      <span className={base}>
        <UserIcon className="size-4" />
      </span>
    );
  }
  if (user.avatar && !broken) {
    // eslint-disable-next-line @next/next/no-img-element -- 静态导出, 头像是外部地址
    return <img src={user.avatar} alt="" className={cn(base, "object-cover")} onError={() => setBroken(true)} />;
  }
  return <span className={base}>{name.slice(0, 1).toUpperCase()}</span>;
}
