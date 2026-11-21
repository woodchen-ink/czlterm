"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useState } from "react";
import {
  AlertTriangleIcon,
  CloudIcon,
  CopyIcon,
  ExternalLinkIcon,
  FingerprintIcon,
  HardDriveIcon,
  KeyRoundIcon,
  LaptopIcon,
  Loader2Icon,
  LogOutIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  TrashIcon,
  UsersIcon,
} from "lucide-react";
import { toast } from "sonner";

import { spaceLabel, useCloud } from "@/components/cloud/cloud-provider";
import { ApproveDeviceDialog, RecoveryCodeDialog, UnlockDialog } from "@/components/cloud/key-dialogs";
import { planStorage, PlansDialog } from "@/components/cloud/plans-dialog";
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
import { api, type DeviceRequest, type SpaceView } from "@/lib/api";
import { errorText, parseError } from "@/lib/errors";
import { formatAgo, formatBytes, formatDate } from "@/lib/format";
import { cn } from "@/lib/utils";

/** 设置 → 账号与同步: 登录、端到端加密的开通与解锁、各空间同步状态、个人订阅、搬移本机数据。 */
export function AccountPanel({ onOpenTeams }: { onOpenTeams: () => void }) {
  useLanguage();
  const { account, user, cloud, spaces, billing, refreshCloud, refreshSpaces } = useCloud();
  const [loginURL, setLoginURL] = useState("");
  const [recovery, setRecovery] = useState("");
  const [unlockOpen, setUnlockOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const [devices, setDevices] = useState<DeviceRequest[]>([]);
  const [approving, setApproving] = useState<DeviceRequest | null>(null);
  const [confirm, setConfirm] = useState<null | { title: string; body: string; action: string; danger?: boolean; run: () => Promise<void> }>(null);
  const [plansOpen, setPlansOpen] = useState(false);

  const keyStatus = cloud?.keyStatus ?? "unknown";
  const unlocked = keyStatus === "unlocked";

  // 登录结果经事件回来, 收到后收起等待态
  useEffect(() => {
    if (account?.loggedIn) queueMicrotask(() => setLoginURL(""));
  }, [account?.loggedIn]);

  // 已解锁时轮询待批准的新设备 (面板开着才查)
  const loadDevices = useCallback(() => {
    api.CloudPendingDevices().then((d) => setDevices(d ?? []), () => {});
  }, []);
  useEffect(() => {
    if (!unlocked) return;
    queueMicrotask(loadDevices);
    const timer = setInterval(loadDevices, 8000);
    return () => clearInterval(timer);
  }, [unlocked, loadDevices]);

  useEffect(() => {
    if (account?.loggedIn) void refreshCloud().catch(() => {});
  }, [account?.loggedIn, refreshCloud]);

  async function run(name: string, fn: () => Promise<void>) {
    setBusy(name);
    try {
      await fn();
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy("");
    }
  }

  async function login() {
    await run("login", async () => setLoginURL(await api.Login()));
  }

  async function logout(force: boolean) {
    try {
      await api.Logout(force);
      toast.success(t("已退出登录"));
    } catch (e) {
      if (!force && parseError(e).code === "6108") {
        setConfirm({
          title: t("还有修改没上传"),
          body: t("同步空间里还有没上传到云端的修改。退出后它们留在本机, 用同一账号重新登录并解锁后会继续上传。"),
          action: t("仍然退出"),
          run: () => logout(true),
        });
        return;
      }
      toast.error(errorText(e));
    }
  }

  if (!account) {
    return (
      <div className="text-muted-foreground flex items-center justify-center py-16">
        <Loader2Icon className="animate-spin" />
      </div>
    );
  }

  if (!account.configured) {
    return (
      <Section icon={CloudIcon} title={t("云同步")}>
        <p className="text-muted-foreground text-sm">{t("云同步与团队共享即将开放。连接与脚本目前只保存在本机。")}</p>
      </Section>
    );
  }

  if (!account.loggedIn || !user) {
    return (
      <div className="space-y-5">
        <div className="bg-card space-y-4 rounded-xl border p-5 shadow-xs">
          <div className="flex items-center gap-3">
            <div className="bg-brand-soft text-brand-strong flex size-10 items-center justify-center rounded-lg">
              <CloudIcon className="size-5" />
            </div>
            <div>
              <h2 className="font-semibold">{t("登录 CZL Connect, 开启云同步")}</h2>
              <p className="text-muted-foreground text-sm">{t("登录是可选的: 不登录时所有本机功能照常使用。")}</p>
            </div>
          </div>
          <ul className="grid gap-2 text-sm sm:grid-cols-3">
            <Benefit icon={LaptopIcon} text={t("多台电脑同步连接与脚本")} />
            <Benefit icon={UsersIcon} text={t("和团队共享服务器")} />
            <Benefit icon={ShieldCheckIcon} text={t("端到端加密, 服务端只见密文")} />
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
            <Button onClick={login} disabled={busy === "login"}>
              {busy === "login" && <Loader2Icon className="animate-spin" />} {t("登录")}
            </Button>
          )}
        </div>
      </div>
    );
  }

  const personal = spaces?.spaces.find((s) => s.kind === "personal");
  const teams = spaces?.spaces.filter((s) => s.kind === "org") ?? [];
  const local = spaces?.spaces.find((s) => s.kind === "local");
  const ent = billing?.entitlements?.personal;
  const personalPlans = (billing?.plans ?? []).filter((p) => p.subjectType === "user" && (!p.status || p.status === "active"));
  const personalInfo = cloud?.spaces.find((s) => s.kind === "personal");

  return (
    <div className="space-y-5">
      {/* 账号 */}
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

      {/* 端到端加密 */}
      {!cloud?.available ? (
        <Notice tone="warning" icon={AlertTriangleIcon}>
          <span className="flex-1">{cloud?.error ? errorText(cloud.error) : t("正在连接同步服务…")}</span>
          <Button size="sm" variant="outline" onClick={() => run("refresh", refreshCloud)} disabled={busy === "refresh"}>
            {busy === "refresh" ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} {t("重试")}
          </Button>
        </Notice>
      ) : keyStatus === "not_enrolled" ? (
        <Section icon={ShieldCheckIcon} title={t("开通云同步")}>
          <p className="text-muted-foreground text-sm">
            {t("数据在本机加密后再上传, 密钥只在你的设备上, CZL 与服务器运维都看不到连接地址和脚本内容。开通时会生成一个恢复码, 换电脑时用它解锁。")}
          </p>
          <Button onClick={() => run("enroll", async () => setRecovery(await api.CloudEnroll()))} disabled={busy === "enroll"}>
            {busy === "enroll" && <Loader2Icon className="animate-spin" />} {t("开通并生成恢复码")}
          </Button>
        </Section>
      ) : keyStatus === "locked" ? (
        <Notice tone="info" icon={KeyRoundIcon}>
          <span className="flex-1">{t("这台电脑还没解锁云同步: 用恢复码, 或让另一台已登录的设备批准。")}</span>
          <Button size="sm" onClick={() => setUnlockOpen(true)}>
            {t("解锁")}
          </Button>
        </Notice>
      ) : null}

      {/* 待批准的新设备 */}
      {unlocked && devices.length > 0 && (
        <Notice tone="info" icon={LaptopIcon}>
          <div className="flex-1 space-y-1">
            {devices.map((d) => (
              <div key={d.id} className="flex items-center gap-2">
                <span className="flex-1 text-sm">{t("「{0}」请求解锁云同步", d.deviceName)}</span>
                <Button size="xs" onClick={() => setApproving(d)}>
                  {t("核对并批准")}
                </Button>
              </div>
            ))}
          </div>
        </Notice>
      )}

      {/* 空间 */}
      {unlocked && (
        <Section
          icon={CloudIcon}
          title={t("同步空间")}
          action={
            <Button
              size="sm"
              variant="outline"
              disabled={busy === "sync"}
              onClick={() =>
                run("sync", async () => {
                  await api.CloudSyncNow();
                  await refreshSpaces();
                  toast.success(t("已同步"));
                })
              }
            >
              <RefreshCwIcon className={cn(busy === "sync" && "animate-spin")} /> {t("立即同步")}
            </Button>
          }
        >
          <div className="divide-y rounded-lg border">
            {personal && (
              <SpaceRow
                s={personal}
                usage={personalInfo ? [personalInfo.usageBytes, personalInfo.quotaBytes] : undefined}
                onSubscribe={() => setPlansOpen(true)}
              />
            )}
            {teams.map((s) => (
              <SpaceRow
                key={s.key}
                s={s}
                onSubscribe={onOpenTeams}
                onForget={
                  s.removed
                    ? () =>
                        setConfirm({
                          title: t("清除「{0}」的本机副本?", spaceLabel(s)),
                          body: t("你已不在这个团队, 本机留着的只读副本会被删除, 无法恢复。"),
                          action: t("清除"),
                          danger: true,
                          run: async () => {
                            await api.CloudForgetSpace(s.key, true);
                            await refreshSpaces();
                          },
                        })
                    : undefined
                }
              />
            ))}
            {teams.length === 0 && (
              <div className="text-muted-foreground flex items-center justify-between gap-2 px-3 py-2.5 text-sm">
                <span>{t("还没有团队空间")}</span>
                <Button size="xs" variant="ghost" onClick={onOpenTeams}>
                  <UsersIcon /> {t("创建或加入团队")}
                </Button>
              </div>
            )}
          </div>

          {/* 本机数据搬进个人同步 */}
          {personal && local && local.connections + local.scripts > 0 && !personal.readOnly && (
            <div className="bg-secondary flex items-center gap-3 rounded-lg p-3 text-sm">
              <HardDriveIcon className="text-muted-foreground size-4 shrink-0" />
              <span className="flex-1">
                {t("本机还有 {0} 条连接、{1} 个脚本没有同步。", local.connections, local.scripts)}
              </span>
              <Button
                size="sm"
                variant="outline"
                onClick={() =>
                  setConfirm({
                    title: t("把本机数据移到「我的云同步」?"),
                    body: t("全部连接与脚本会移进个人同步空间并加密上传, 跳板关系保持不变; 本机空间随后为空。之后在任何登录同一账号的电脑上都能用。"),
                    action: t("移过去"),
                    run: async () => {
                      const r = await api.CloudMoveLocalToPersonal();
                      await refreshSpaces();
                      toast.success(t("已移动 {0} 条连接、{1} 个脚本", r.connections, r.scripts));
                    },
                  })
                }
              >
                {t("移到云同步")}
              </Button>
            </div>
          )}
        </Section>
      )}

      {/* 个人订阅 */}
      {billing?.enabled && (
        <Section
          icon={CloudIcon}
          title={t("个人订阅")}
          action={
            personalPlans.length > 0 && (
              <Button size="sm" onClick={() => setPlansOpen(true)}>
                {ent?.active ? t("续费或升级") : t("开通")}
              </Button>
            )
          }
        >
          {ent?.active ? (
            <p className="text-sm">
              <span className="font-medium">{ent.planName || ent.plan}</span>
              <span className="text-muted-foreground">
                {" · "}
                {t("有效期至 {0}", formatDate(ent.periodEnd))}
                {planStorage(ent.quotas) > 0 && ` · ${t("{0} GB 存储", planStorage(ent.quotas))}`}
                {ent.cancelAtPeriodEnd && ` · ${t("到期后不续费")}`}
              </span>
            </p>
          ) : (
            <p className="text-muted-foreground text-sm">
              {ent && !ent.active
                ? t("订阅已到期: 个人空间现在只读, 数据都在, 续费后恢复同步。")
                : t("还没有订阅。开通后个人空间可在多台电脑间同步; 团队空间在「团队」里单独订阅。")}
            </p>
          )}
        </Section>
      )}

      {/* 密钥 */}
      {unlocked && (
        <Section icon={FingerprintIcon} title={t("加密密钥")}>
          <p className="text-muted-foreground text-sm">
            {t("本机指纹")}: <span className="text-foreground font-mono">{cloud?.fingerprint}</span>
          </p>
          <p className="text-muted-foreground text-xs">{t("团队成员可互相核对指纹, 确认封装密钥的是同一个人。")}</p>
          <Button
            size="sm"
            variant="outline"
            onClick={() =>
              setConfirm({
                title: t("重置恢复码?"),
                body: t("会生成新的恢复码, 旧的立即作废。已解锁的设备不受影响。"),
                action: t("重置"),
                run: async () => setRecovery(await api.CloudResetRecoveryCode()),
              })
            }
          >
            <KeyRoundIcon /> {t("重置恢复码")}
          </Button>
        </Section>
      )}

      <RecoveryCodeDialog
        code={recovery}
        onDone={() => {
          setRecovery("");
          void refreshCloud();
        }}
      />
      <UnlockDialog
        open={unlockOpen}
        onOpenChange={setUnlockOpen}
        onUnlocked={() => {
          setUnlockOpen(false);
          void refreshCloud();
        }}
      />
      <ApproveDeviceDialog
        request={approving}
        onClose={() => {
          setApproving(null);
          loadDevices();
        }}
      />
      <PlansDialog
        open={plansOpen}
        onOpenChange={setPlansOpen}
        subject="user"
        plans={personalPlans}
        currentPlan={ent?.active ? ent.plan : undefined}
        title={t("个人云同步")}
      />
      <AlertDialog open={!!confirm} onOpenChange={(o) => !o && setConfirm(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{confirm?.title}</AlertDialogTitle>
            <AlertDialogDescription>{confirm?.body}</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
            <AlertDialogAction
              variant={confirm?.danger ? "destructive" : "default"}
              onClick={() => {
                const c = confirm;
                setConfirm(null);
                if (c) void run("confirm", c.run);
              }}
            >
              {confirm?.action}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

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

function SpaceRow({
  s,
  usage,
  onSubscribe,
  onForget,
}: {
  s: SpaceView;
  usage?: [number, number];
  onSubscribe: () => void;
  onForget?: () => void;
}) {
  useLanguage();
  const st = spaceStatus(s);
  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      {s.kind === "personal" ? <LaptopIcon className="text-muted-foreground size-4" /> : <UsersIcon className="text-muted-foreground size-4" />}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">
          {spaceLabel(s)}
          {s.kind === "org" && s.role && <span className="text-muted-foreground ml-2 text-xs font-normal">{roleLabel(s.role)}</span>}
        </p>
        <p className="text-muted-foreground flex items-center gap-1.5 truncate text-xs">
          <StatusDot tone={st.tone} /> <span className="truncate">{st.text}</span>
          <span>· {t("{0} 条连接", s.connections)}</span>
          {usage && usage[0] > 0 && <span>· {usage[1] > 0 ? `${formatBytes(usage[0])} / ${formatBytes(usage[1])}` : formatBytes(usage[0])}</span>}
        </p>
      </div>
      {s.need && (
        <Button size="xs" onClick={onSubscribe}>
          {t("订阅")}
        </Button>
      )}
      {onForget && (
        <Button size="xs" variant="ghost" className="text-destructive" onClick={onForget}>
          <TrashIcon /> {t("清除")}
        </Button>
      )}
    </div>
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

function Notice({
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

function Benefit({ icon: Icon, text }: { icon: React.ComponentType<{ className?: string }>; text: string }) {
  return (
    <li className="bg-secondary flex items-center gap-2 rounded-md px-3 py-2">
      <Icon className="text-muted-foreground size-4 shrink-0" />
      <span>{text}</span>
    </li>
  );
}
