"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useState } from "react";
import {
  AlertTriangleIcon,
  CheckIcon,
  CloudIcon,
  FingerprintIcon,
  HardDriveIcon,
  KeyRoundIcon,
  LaptopIcon,
  Loader2Icon,
  PackageIcon,
  RefreshCwIcon,
  ShieldCheckIcon,
  TrashIcon,
  UsersIcon,
} from "lucide-react";
import { toast } from "sonner";

import { LoginCard } from "@/components/cloud/account-panel";
import { spaceLabel, useCloud } from "@/components/cloud/cloud-provider";
import { ApproveDeviceDialog, RecoveryCodeDialog, UnlockDialog } from "@/components/cloud/key-dialogs";
import { AddOnDialog, addOnUnit, planStorage, PlansDialog } from "@/components/cloud/plans-dialog";
import { Loading, Notice, roleLabel, Section, spaceStatus, StatusDot } from "@/components/cloud/shared";
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
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { api, type DeviceRequest, type Entitlement, type Plan, type SpaceView } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes, formatDate, formatMoney } from "@/lib/format";
import { cn } from "@/lib/utils";

type Buy = { subject: string; title: string; plans: Plan[]; current?: string; initial?: string };
type Confirm = { title: string; body: string; action: string; danger?: boolean; run: () => Promise<void> };

const activePlan = (p: Plan) => !p.status || p.status === "active";

/**
 * 设置 → 云同步: 套餐与权益、订阅的开通 / 续费 / 加购、端到端加密的开通与解锁、各空间同步状态。
 * 未登录时只展示登录卡片与套餐。
 */
export function SyncPanel({ onOpenTeams }: { onOpenTeams: () => void }) {
  useLanguage();
  const { account, user, cloud, spaces, billing, refreshCloud, refreshSpaces } = useCloud();
  const [recovery, setRecovery] = useState("");
  const [unlockOpen, setUnlockOpen] = useState(false);
  const [busy, setBusy] = useState("");
  const [devices, setDevices] = useState<DeviceRequest[]>([]);
  const [approving, setApproving] = useState<DeviceRequest | null>(null);
  const [confirm, setConfirm] = useState<Confirm | null>(null);
  const [buy, setBuy] = useState<Buy | null>(null);
  const [addOn, setAddOn] = useState<{ subject: string; title: string } | null>(null);

  const keyStatus = cloud?.keyStatus ?? "unknown";
  const unlocked = keyStatus === "unlocked";

  // 已解锁时轮询待批准的新设备 (页面开着才查)
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

  if (!account) return <Loading />;
  if (!account.configured) {
    return (
      <Section icon={CloudIcon} title={t("云同步")}>
        <p className="text-muted-foreground text-sm">{t("云同步与团队共享即将开放。连接与脚本目前只保存在本机。")}</p>
      </Section>
    );
  }

  const plans = (billing?.plans ?? []).filter(activePlan).sort((a, b) => a.sortOrder - b.sortOrder);
  const userPlans = plans.filter((p) => p.subjectType === "user");
  const orgPlans = plans.filter((p) => p.subjectType === "org");
  const ent = billing?.entitlements?.personal;
  const orgs = billing?.entitlements?.orgs ?? [];
  const adminOrgs = orgs.filter((o) => o.role === "owner" || o.role === "admin");

  const buyPersonal = (initial?: string) =>
    setBuy({ subject: "user", title: t("个人云同步"), plans: userPlans, current: ent?.active ? ent.plan : undefined, initial });
  const buyOrg = (orgId: string, initial?: string) => {
    const o = orgs.find((x) => x.orgId === orgId);
    if (!o) return;
    const e = o.entitlement;
    setBuy({ subject: `org:${orgId}`, title: t("「{0}」团队订阅", o.name), plans: orgPlans, current: e?.active ? e.plan : undefined, initial });
  };

  const planGrid = plans.length > 0 && (
    <Section icon={PackageIcon} title={t("套餐与权益")}>
      <div className="grid gap-2 sm:grid-cols-3">
        {plans.map((p) => {
          const org = p.subjectType === "org";
          const current = org ? orgs.some((o) => o.entitlement?.active && o.entitlement.plan === p.code) : ent?.active && ent.plan === p.code;
          return (
            <PlanCard key={p.code} plan={p} current={!!current}>
              {!user ? null : !org ? (
                <Button size="sm" variant={current ? "outline" : "default"} className="w-full" onClick={() => buyPersonal(p.code)}>
                  {current ? t("续费") : t("选择")}
                </Button>
              ) : adminOrgs.length === 0 ? (
                <Button
                  size="sm"
                  variant="outline"
                  className="w-full"
                  onClick={() => {
                    toast.info(orgs.length > 0 ? t("团队订阅由所有者或管理员开通") : t("先创建一个团队, 再为它开通"));
                    if (orgs.length === 0) onOpenTeams();
                  }}
                >
                  {orgs.length > 0 ? t("选择") : t("先创建团队")}
                </Button>
              ) : adminOrgs.length === 1 ? (
                <Button size="sm" variant={current ? "outline" : "default"} className="w-full" onClick={() => buyOrg(adminOrgs[0].orgId, p.code)}>
                  {current ? t("续费") : t("选择")}
                </Button>
              ) : (
                <DropdownMenu>
                  <DropdownMenuTrigger asChild>
                    <Button size="sm" className="w-full">
                      {t("选择")}
                    </Button>
                  </DropdownMenuTrigger>
                  <DropdownMenuContent align="end">
                    <DropdownMenuLabel className="text-muted-foreground text-xs font-normal">{t("为哪个团队开通")}</DropdownMenuLabel>
                    {adminOrgs.map((o) => (
                      <DropdownMenuItem key={o.orgId} onSelect={() => buyOrg(o.orgId, p.code)}>
                        <UsersIcon /> {o.name}
                      </DropdownMenuItem>
                    ))}
                  </DropdownMenuContent>
                </DropdownMenu>
              )}
            </PlanCard>
          );
        })}
      </div>
      {(billing?.addons?.length ?? 0) > 0 && (
        <p className="text-muted-foreground text-xs">
          {t("已订阅的可随时加购:")}{" "}
          {billing!.addons
            .map((a) => `${addOnUnit(a)} ${formatMoney(a.priceCents, a.currency)}/${t("年")}`)
            .join(t("、"))}
        </p>
      )}
    </Section>
  );

  if (!account.loggedIn || !user) {
    return (
      <div className="space-y-6">
        <LoginCard />
        {planGrid}
      </div>
    );
  }

  const personalSpace = spaces?.spaces.find((s) => s.kind === "personal");
  const teamSpaces = spaces?.spaces.filter((s) => s.kind === "org") ?? [];
  const local = spaces?.spaces.find((s) => s.kind === "local");
  const personalInfo = cloud?.spaces.find((s) => s.kind === "personal");

  return (
    <div className="space-y-6">
      {/* 端到端加密: 开通 / 解锁 */}
      {!cloud?.available ? (
        <Notice tone="warning" icon={AlertTriangleIcon}>
          <span className="flex-1">{cloud?.error ? errorText(cloud.error) : t("正在连接同步服务…")}</span>
          <Button size="sm" variant="outline" onClick={() => run("refresh", refreshCloud)} disabled={busy === "refresh"}>
            {busy === "refresh" ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} {t("重试")}
          </Button>
        </Notice>
      ) : keyStatus === "not_enrolled" ? (
        <div className="bg-card space-y-3 rounded-xl border p-4 shadow-xs">
          <div className="flex items-center gap-3">
            <div className="bg-brand-soft text-brand-strong flex size-9 shrink-0 items-center justify-center rounded-lg">
              <ShieldCheckIcon className="size-5" />
            </div>
            <div className="min-w-0 flex-1">
              <p className="font-medium">{t("开启端到端加密")}</p>
              <p className="text-muted-foreground text-xs">{t("个人空间和团队空间都要先开启才能同步, 一个账号只需开启一次, 与是否订阅无关。密钥只在你的设备上, 服务器看不到连接地址、凭据和脚本。")}</p>
            </div>
          </div>
          <Button size="sm" onClick={() => run("enroll", async () => setRecovery(await api.CloudEnroll()))} disabled={busy === "enroll"}>
            {busy === "enroll" && <Loader2Icon className="animate-spin" />} {t("开启并生成恢复码")}
          </Button>
        </div>
      ) : keyStatus === "locked" ? (
        <Notice tone="info" icon={KeyRoundIcon}>
          <span className="flex-1">{t("这台电脑还没解锁云同步: 用恢复码, 或让另一台已登录的设备批准。")}</span>
          <Button size="sm" onClick={() => setUnlockOpen(true)}>
            {t("解锁")}
          </Button>
        </Notice>
      ) : null}

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

      {/* 订阅 */}
      {billing?.enabled && (
        <Section icon={CloudIcon} title={t("我的订阅")}>
          <div className="divide-y rounded-lg border">
            <SubscriptionRow
              icon={LaptopIcon}
              name={t("个人空间")}
              ent={ent}
              empty={t("未订阅, 个人空间暂不能同步")}
              canManage={userPlans.length > 0}
              onBuy={() => buyPersonal()}
              onAddOn={() => setAddOn({ subject: "user", title: t("个人空间加购") })}
            />
            {orgs.map((o) => (
              <SubscriptionRow
                key={o.orgId}
                icon={UsersIcon}
                name={o.name}
                tag={roleLabel(o.role)}
                ent={o.entitlement}
                seatsUsed={o.memberCount}
                empty={t("未订阅: 团队空间只读")}
                canManage={(o.role === "owner" || o.role === "admin") && orgPlans.length > 0}
                onBuy={() => buyOrg(o.orgId)}
                onAddOn={() => setAddOn({ subject: `org:${o.orgId}`, title: t("「{0}」加购", o.name) })}
              />
            ))}
            {orgs.length === 0 && (
              <div className="text-muted-foreground flex items-center justify-between gap-2 px-3 py-2.5 text-sm">
                <span>{t("还没有团队")}</span>
                <Button size="xs" variant="ghost" onClick={onOpenTeams}>
                  <UsersIcon /> {t("创建或加入团队")}
                </Button>
              </div>
            )}
          </div>
        </Section>
      )}

      {planGrid}

      {/* 同步空间 */}
      {unlocked && (
        <Section
          icon={RefreshCwIcon}
          title={t("同步状态")}
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
            {personalSpace && <SpaceRow s={personalSpace} usage={personalInfo ? [personalInfo.usageBytes, personalInfo.quotaBytes] : undefined} />}
            {teamSpaces.map((s) => (
              <SpaceRow
                key={s.key}
                s={s}
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
          </div>

          {personalSpace && local && local.connections + local.scripts > 0 && !personalSpace.readOnly && (
            <div className="bg-secondary flex items-center gap-3 rounded-lg p-3 text-sm">
              <HardDriveIcon className="text-muted-foreground size-4 shrink-0" />
              <span className="flex-1">{t("本机还有 {0} 条连接、{1} 个脚本没有同步。", local.connections, local.scripts)}</span>
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

      {/* 密钥 */}
      {unlocked && (
        <Section icon={FingerprintIcon} title={t("加密密钥")}>
          <div className="flex flex-wrap items-center gap-3 rounded-lg border px-3 py-2.5">
            <div className="min-w-0 flex-1">
              <p className="text-sm">
                {t("本机指纹")} <span className="font-mono">{cloud?.fingerprint}</span>
              </p>
              <p className="text-muted-foreground text-xs">{t("团队成员可互相核对指纹, 确认封装密钥的是同一个人。")}</p>
            </div>
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
          </div>
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
        open={!!buy}
        onOpenChange={(o) => !o && setBuy(null)}
        subject={buy?.subject ?? "user"}
        plans={buy?.plans ?? []}
        currentPlan={buy?.current}
        initialPlan={buy?.initial}
        title={buy?.title ?? ""}
      />
      <AddOnDialog
        open={!!addOn}
        onOpenChange={(o) => !o && setAddOn(null)}
        subject={addOn?.subject ?? "user"}
        title={addOn?.title ?? ""}
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

/** 套餐卡片: 价格与权益清单, 底部放购买按钮。 */
function PlanCard({ plan: p, current, children }: { plan: Plan; current: boolean; children?: React.ReactNode }) {
  useLanguage();
  const gb = planStorage(p.quotas);
  const org = p.subjectType === "org";
  const perks = [
    gb > 0 ? t("{0} GB 加密存储", gb) : "",
    org ? t("{0} 位成员", p.seats) : t("不限电脑数量"),
    org ? t("团队共享连接、脚本与凭据") : t("连接、脚本与凭据多机同步"),
    org ? t("成员角色与邀请") : "",
    t("端到端加密"),
  ].filter(Boolean);
  return (
    <div className={cn("flex flex-col gap-3 rounded-lg border p-3", current && "border-brand bg-brand-soft/40")}>
      <div>
        <div className="flex items-center justify-between gap-2">
          <span className="font-medium">{p.name}</span>
          {current && <span className="text-brand-strong text-xs">{t("当前套餐")}</span>}
        </div>
        <p className="mt-1 text-lg font-semibold">
          {formatMoney(p.priceCents, p.currency)}
          <span className="text-muted-foreground text-xs font-normal"> / {p.periodUnit === "month" ? t("月") : t("年")}</span>
        </p>
      </div>
      <ul className="flex-1 space-y-1 text-xs">
        {perks.map((x) => (
          <li key={x} className="flex gap-1.5">
            <CheckIcon className="text-success mt-px size-3.5 shrink-0" />
            <span>{x}</span>
          </li>
        ))}
      </ul>
      {children}
    </div>
  );
}

function SubscriptionRow({
  icon: Icon,
  name,
  tag,
  ent,
  seatsUsed,
  empty,
  canManage,
  onBuy,
  onAddOn,
}: {
  icon: React.ComponentType<{ className?: string }>;
  name: string;
  tag?: string;
  ent?: Entitlement;
  seatsUsed?: number;
  empty: string;
  canManage: boolean;
  onBuy: () => void;
  onAddOn: () => void;
}) {
  useLanguage();
  const active = !!ent?.active;
  const gb = planStorage(ent?.quotas);
  const detail = active
    ? [
        ent!.planName || ent!.plan,
        t("{0} 到期", formatDate(ent!.periodEnd)),
        seatsUsed !== undefined && ent!.seats > 0 ? t("{0}/{1} 席", seatsUsed, ent!.seats) : "",
        gb > 0 ? `${gb} GB` : "",
        ent!.cancelAtPeriodEnd ? t("到期后不续费") : "",
      ]
        .filter(Boolean)
        .join(" · ")
    : ent
      ? t("已到期: 现在只读, 数据都在, 续费后恢复同步")
      : empty;
  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      <Icon className="text-muted-foreground size-4 shrink-0" />
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">
          {name}
          {tag && <span className="text-muted-foreground ml-2 text-xs font-normal">{tag}</span>}
        </p>
        <p className={cn("flex items-center gap-1.5 truncate text-xs", active ? "text-muted-foreground" : "text-warning")}>
          {!active && <StatusDot tone="warn" />}
          <span className="truncate">{detail}</span>
        </p>
      </div>
      {canManage ? (
        <div className="flex shrink-0 gap-1">
          {active && (
            <Button size="xs" variant="ghost" onClick={onAddOn}>
              {t("加购")}
            </Button>
          )}
          <Button size="xs" variant={active ? "outline" : "default"} onClick={onBuy}>
            {active ? t("续费或升级") : t("开通")}
          </Button>
        </div>
      ) : (
        tag && <span className="text-muted-foreground shrink-0 text-xs">{t("由管理员管理")}</span>
      )}
    </div>
  );
}

function SpaceRow({ s, usage, onForget }: { s: SpaceView; usage?: [number, number]; onForget?: () => void }) {
  useLanguage();
  const st = spaceStatus(s);
  return (
    <div className="flex items-center gap-3 px-3 py-2.5">
      {s.kind === "personal" ? <LaptopIcon className="text-muted-foreground size-4" /> : <UsersIcon className="text-muted-foreground size-4" />}
      <div className="min-w-0 flex-1">
        <p className="truncate text-sm font-medium">{spaceLabel(s)}</p>
        <p className="text-muted-foreground flex items-center gap-1.5 truncate text-xs">
          <StatusDot tone={st.tone} /> <span className="truncate">{st.text}</span>
          <span>· {t("{0} 条连接", s.connections)}</span>
          {usage && usage[0] > 0 && <span>· {usage[1] > 0 ? `${formatBytes(usage[0])} / ${formatBytes(usage[1])}` : formatBytes(usage[0])}</span>}
        </p>
      </div>
      {onForget && (
        <Button size="xs" variant="ghost" className="text-destructive" onClick={onForget}>
          <TrashIcon /> {t("清除")}
        </Button>
      )}
    </div>
  );
}
