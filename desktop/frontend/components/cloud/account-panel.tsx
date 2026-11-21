"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useEffect, useState } from "react";
import { CloudIcon, CopyIcon, ExternalLinkIcon, LaptopIcon, Loader2Icon, LogOutIcon, ShieldCheckIcon, UsersIcon } from "lucide-react";
import { toast } from "sonner";

import { useCloud } from "@/components/cloud/cloud-provider";
import { LinkRow, Loading, Section } from "@/components/cloud/shared";
import { UserAvatar } from "@/components/cloud/user-avatar";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { api } from "@/lib/api";
import { errorText, parseError } from "@/lib/errors";

/** 设置 → 账号: 登录 / 退出, 以及去云同步与团队的入口。同步与订阅的细节在「云同步」页。 */
export function AccountPanel({ onOpenSync, onOpenTeams }: { onOpenSync: () => void; onOpenTeams: () => void }) {
  useLanguage();
  const { account, user, cloud, billing } = useCloud();
  const [unsynced, setUnsynced] = useState(false);

  async function logout(force: boolean) {
    try {
      await api.Logout(force);
      toast.success(t("已退出登录"));
    } catch (e) {
      if (!force && parseError(e).code === "6108") {
        setUnsynced(true);
        return;
      }
      toast.error(errorText(e));
    }
  }

  if (!account) return <Loading />;
  if (!account.configured) {
    return (
      <Section icon={CloudIcon} title={t("账号")}>
        <p className="text-muted-foreground text-sm">{t("云同步与团队共享即将开放。连接与脚本目前只保存在本机。")}</p>
      </Section>
    );
  }
  if (!account.loggedIn || !user) return <LoginCard />;

  const keyStatus = cloud?.keyStatus ?? "unknown";
  const personal = billing?.entitlements?.personal;
  const teams = billing?.entitlements?.orgs ?? [];
  const syncLine = !cloud?.available
    ? { tone: "warn" as const, text: t("同步服务暂不可用") }
    : keyStatus === "not_enrolled"
      ? { tone: "idle" as const, text: t("未开启端到端加密") }
      : keyStatus === "locked"
        ? { tone: "warn" as const, text: t("这台电脑待解锁") }
        : personal?.active
          ? { tone: "ok" as const, text: `${personal.planName || personal.plan} · ${t("已开启")}` }
          : { tone: "idle" as const, text: t("已开启, 个人空间未订阅") };

  return (
    <div className="space-y-5">
      <div className="bg-card flex items-center gap-3 rounded-xl border p-4 shadow-xs">
        <UserAvatar user={user} className="size-10" />
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{user.nickname || user.username || user.email}</p>
          <p className="text-muted-foreground truncate text-sm">
            {user.email}
            {account.offline && ` · ${t("离线, 使用本机登录记录")}`}
          </p>
        </div>
        <Button variant="outline" size="sm" onClick={() => logout(false)}>
          <LogOutIcon /> {t("退出登录")}
        </Button>
      </div>

      <div className="divide-y overflow-hidden rounded-lg border">
        <LinkRow icon={CloudIcon} title={t("云同步")} text={syncLine.text} tone={syncLine.tone} onClick={onOpenSync} />
        <LinkRow
          icon={UsersIcon}
          title={t("团队")}
          text={teams.length > 0 ? teams.map((o) => o.name).join("、") : t("还没有团队")}
          onClick={onOpenTeams}
        />
      </div>

      <AlertDialog open={unsynced} onOpenChange={setUnsynced}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("还有修改没上传")}</AlertDialogTitle>
            <AlertDialogDescription>
              {t("同步空间里还有没上传到云端的修改。退出后它们留在本机, 用同一账号重新登录并解锁后会继续上传。")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                setUnsynced(false);
                void logout(true);
              }}
            >
              {t("仍然退出")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

/** 未登录时的登录卡片: 账号页与云同步页共用。 */
export function LoginCard() {
  useLanguage();
  const { account } = useCloud();
  const [loginURL, setLoginURL] = useState("");
  const [busy, setBusy] = useState(false);

  // 登录结果经事件回来, 收到后收起等待态
  useEffect(() => {
    if (account?.loggedIn) queueMicrotask(() => setLoginURL(""));
  }, [account?.loggedIn]);

  async function login() {
    setBusy(true);
    try {
      setLoginURL(await api.Login());
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="bg-card space-y-4 rounded-xl border p-5 shadow-xs">
      <div className="flex items-center gap-3">
        <div className="bg-brand-soft text-brand-strong flex size-10 items-center justify-center rounded-lg">
          <CloudIcon className="size-5" />
        </div>
        <div>
          <h2 className="font-semibold">{t("登录 CZL Connect")}</h2>
          <p className="text-muted-foreground text-sm">{t("开启云同步与团队共享。不登录也不影响本机的任何功能。")}</p>
        </div>
      </div>
      <ul className="grid gap-2 text-sm sm:grid-cols-3">
        <Benefit icon={LaptopIcon} title={t("多台电脑同步")} text={t("连接、脚本与凭据随时一致")} />
        <Benefit icon={UsersIcon} title={t("团队共享")} text={t("和同事共用服务器与凭据")} />
        <Benefit icon={ShieldCheckIcon} title={t("端到端加密")} text={t("数据在本机加密, 服务器只存密文")} />
      </ul>
      {loginURL ? (
        <div className="bg-secondary space-y-2 rounded-lg p-3 text-sm">
          <p className="flex items-center gap-2">
            <Loader2Icon className="size-4 animate-spin" /> {t("已在浏览器打开授权页, 完成后会自动回到这里")}
          </p>
          <div className="flex flex-wrap gap-1.5">
            <Button size="sm" variant="outline" onClick={() => navigator.clipboard.writeText(loginURL).then(() => toast.success(t("已复制")))}>
              <CopyIcon /> {t("复制授权链接")}
            </Button>
            <Button size="sm" variant="outline" onClick={login}>
              <ExternalLinkIcon /> {t("重新打开")}
            </Button>
            <Button
              size="sm"
              variant="ghost"
              onClick={() => {
                void api.CancelLogin();
                setLoginURL("");
              }}
            >
              {t("取消")}
            </Button>
          </div>
          <p className="text-muted-foreground text-xs">{t("默认浏览器不方便时, 复制链接到任意浏览器打开也能完成登录。")}</p>
        </div>
      ) : (
        <Button onClick={login} disabled={busy}>
          {busy && <Loader2Icon className="animate-spin" />} {t("登录")}
        </Button>
      )}
    </div>
  );
}

function Benefit({ icon: Icon, title, text }: { icon: React.ComponentType<{ className?: string }>; title: string; text: string }) {
  return (
    <li className="bg-secondary flex gap-2.5 rounded-md px-3 py-2.5">
      <Icon className="text-muted-foreground mt-0.5 size-4 shrink-0" />
      <div className="min-w-0">
        <p className="font-medium">{title}</p>
        <p className="text-muted-foreground text-xs">{text}</p>
      </div>
    </li>
  );
}
