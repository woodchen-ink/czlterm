"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useState } from "react";
import { CopyIcon, Loader2Icon, LogOutIcon, MailIcon, PlusIcon, TicketIcon, TrashIcon, UsersIcon } from "lucide-react";
import { toast } from "sonner";

import { roleLabel, Section } from "@/components/cloud/account-panel";
import { useCloud } from "@/components/cloud/cloud-provider";
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
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { api, type NewInviteCode, type Org, type OrgDetail } from "@/lib/api";
import { errorText, parseError } from "@/lib/errors";
import { formatDate } from "@/lib/format";

const DOWNLOAD_URL = "https://github.com/woodchen-ink/czlterm/releases/latest";
const ROLES = ["admin", "member", "viewer"] as const;

/** 设置 → 团队: 建团队、凭码加入、成员与角色、邀请、团队订阅、退出与解散。 */
export function TeamPanel({ onOpenAccount }: { onOpenAccount: () => void }) {
  useLanguage();
  const { user, billing, refreshCloud, openOrgSpace } = useCloud();
  const [orgs, setOrgs] = useState<Org[] | null>(null);
  const [orgId, setOrgId] = useState("");
  const [detail, setDetail] = useState<OrgDetail | null>(null);
  const [error, setError] = useState("");
  const [dialog, setDialog] = useState<"" | "create" | "join">("");

  const loadOrgs = useCallback(async (select?: string) => {
    try {
      const list = (await api.OrgList()) ?? [];
      setOrgs(list);
      setError("");
      setOrgId((cur) => select ?? (list.some((o) => o.id === cur) ? cur : (list[0]?.id ?? "")));
    } catch (e) {
      setError(errorText(e));
      setOrgs([]);
    }
  }, []);

  const loadDetail = useCallback(async () => {
    if (!orgId) {
      setDetail(null);
      return;
    }
    try {
      setDetail(await api.OrgDetail(orgId));
    } catch (e) {
      toast.error(errorText(e));
    }
  }, [orgId]);

  useEffect(() => {
    if (user) queueMicrotask(() => void loadOrgs());
  }, [user, loadOrgs]);
  useEffect(() => {
    queueMicrotask(() => void loadDetail());
  }, [loadDetail]);

  if (!user) {
    return (
      <Section icon={UsersIcon} title={t("团队")}>
        <p className="text-muted-foreground text-sm">{t("团队空间让成员共享连接与脚本, 内容同样端到端加密。先登录 CZL Connect。")}</p>
        <Button size="sm" onClick={onOpenAccount}>
          {t("去登录")}
        </Button>
      </Section>
    );
  }
  if (!orgs) {
    return (
      <div className="text-muted-foreground flex items-center justify-center py-16">
        <Loader2Icon className="animate-spin" />
      </div>
    );
  }

  const afterJoin = async (id: string) => {
    setDialog("");
    await loadOrgs(id);
    await refreshCloud();
  };

  return (
    <div className="space-y-5">
      <div className="flex flex-wrap items-center gap-2">
        {orgs.length > 0 && (
          <Select value={orgId} onValueChange={setOrgId}>
            <SelectTrigger className="w-60" aria-label={t("团队")}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {orgs.map((o) => (
                <SelectItem key={o.id} value={o.id}>
                  {o.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        )}
        <div className="flex-1" />
        <Button size="sm" variant="outline" onClick={() => setDialog("join")}>
          <TicketIcon /> {t("凭邀请码加入")}
        </Button>
        <Button size="sm" onClick={() => setDialog("create")}>
          <PlusIcon /> {t("新建团队")}
        </Button>
      </div>

      {error && <p className="text-destructive text-sm">{error}</p>}
      {orgs.length === 0 && !error && (
        <div className="text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm">
          <UsersIcon className="mx-auto mb-2 size-6" />
          {t("还没有团队。新建一个, 或用同事发来的邀请码加入。")}
        </div>
      )}

      {detail && (
        <OrgView
          key={detail.id}
          detail={detail}
          billing={billing}
          onChanged={loadDetail}
          onOpenSpace={() =>
            void openOrgSpace(detail.id).then((ok) =>
              ok ? toast.success(t("已切换到「{0}」", detail.name)) : toast.info(t("团队空间还在准备, 开通云同步并解锁后即可打开")),
            )
          }
          onLeft={async () => {
            await loadOrgs();
            await refreshCloud();
          }}
        />
      )}

      <NameDialog
        open={dialog === "create"}
        title={t("新建团队")}
        label={t("团队名称")}
        action={t("创建")}
        onClose={() => setDialog("")}
        onSubmit={async (name) => {
          const o = await api.OrgCreate(name);
          toast.success(t("已创建团队「{0}」", o.name));
          await afterJoin(o.id);
        }}
      />
      <NameDialog
        open={dialog === "join"}
        title={t("凭邀请码加入")}
        label={t("邀请码")}
        action={t("加入")}
        mono
        onClose={() => setDialog("")}
        onSubmit={async (code) => {
          const r = await api.OrgJoin(code);
          toast.success(r.alreadyMember ? t("你已经在团队「{0}」里", r.org.name) : t("已加入团队「{0}」", r.org.name));
          await afterJoin(r.org.id);
        }}
      />
    </div>
  );
}

function OrgView({
  detail,
  billing,
  onChanged,
  onOpenSpace,
  onLeft,
}: {
  detail: OrgDetail;
  billing: ReturnType<typeof useCloud>["billing"];
  onChanged: () => Promise<void>;
  onOpenSpace: () => void;
  onLeft: () => Promise<void>;
}) {
  useLanguage();
  const admin = detail.role === "owner" || detail.role === "admin";
  const owner = detail.role === "owner";
  const [email, setEmail] = useState("");
  const [inviteRole, setInviteRole] = useState("member");
  const [codeRole, setCodeRole] = useState("member");
  const [newCode, setNewCode] = useState<NewInviteCode | null>(null);
  const [busy, setBusy] = useState("");
  const [plansOpen, setPlansOpen] = useState(false);
  const [confirm, setConfirm] = useState<null | { title: string; body: string; action: string; run: () => Promise<void> }>(null);

  const ent = billing?.entitlements?.orgs?.find((o) => o.orgId === detail.id);
  const plans = (billing?.plans ?? []).filter((p) => p.subjectType === "org" && (!p.status || p.status === "active"));

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

  function inviteText(c: NewInviteCode) {
    return [
      t("邀请你加入 czlterm 团队「{0}」", detail.name),
      t("已安装 czlterm: 点击 {0}", `czlterm://team/join?code=${encodeURIComponent(c.code)}`),
      c.joinUrl ? t("或在网页加入: {0}", c.joinUrl) : "",
      t("邀请码: {0}", c.code),
      t("下载 czlterm: {0}", DOWNLOAD_URL),
    ]
      .filter(Boolean)
      .join("\n");
  }

  function leave(force: boolean) {
    const close = owner;
    const fn = close ? api.OrgClose : api.OrgLeave;
    return fn(detail.id, force).then(
      async () => {
        toast.success(close ? t("团队已解散") : t("已退出团队"));
        await onLeft();
      },
      (e) => {
        if (!force && parseError(e).code === "6406") {
          setConfirm({
            title: t("还有修改没上传"),
            body: t("团队空间里还有没上传的修改, 继续会丢掉它们。"),
            action: t("仍然继续"),
            run: () => leave(true),
          });
          return;
        }
        toast.error(errorText(e));
      },
    );
  }

  return (
    <div className="space-y-6">
      <div className="bg-card flex flex-wrap items-center gap-3 rounded-xl border p-4 shadow-xs">
        <div className="bg-brand-soft text-brand-strong flex size-10 items-center justify-center rounded-lg">
          <UsersIcon className="size-5" />
        </div>
        <div className="min-w-0 flex-1">
          <p className="truncate font-medium">{detail.name}</p>
          <p className="text-muted-foreground text-sm">
            {roleLabel(detail.role)} · {t("{0} 位成员", detail.memberCount)}
            {detail.seats > 0 && ` / ${t("{0} 席", detail.seats)}`}
            {detail.status === "readonly" && ` · ${t("只读")}`}
          </p>
        </div>
        <Button size="sm" variant="outline" onClick={onOpenSpace}>
          {t("打开团队空间")}
        </Button>
      </div>

      {billing?.enabled && (
        <Section
          icon={UsersIcon}
          title={t("团队订阅")}
          action={
            admin && plans.length > 0 && (
              <Button size="sm" onClick={() => setPlansOpen(true)}>
                {ent?.entitlement?.active ? t("续费或升级") : t("开通")}
              </Button>
            )
          }
        >
          {ent?.entitlement?.active ? (
            <p className="text-sm">
              <span className="font-medium">{ent.entitlement.planName || ent.entitlement.plan}</span>
              <span className="text-muted-foreground">
                {" · "}
                {t("有效期至 {0}", formatDate(ent.entitlement.periodEnd))}
                {` · ${t("{0} 席", ent.entitlement.seats)}`}
                {planStorage(ent.entitlement.quotas) > 0 && ` · ${t("{0} GB 存储", planStorage(ent.entitlement.quotas))}`}
              </span>
            </p>
          ) : (
            <p className="text-muted-foreground text-sm">
              {admin ? t("团队还没有有效订阅, 团队空间现在只读。") : t("团队还没有有效订阅, 请联系团队管理员开通。")}
            </p>
          )}
        </Section>
      )}

      <Section icon={UsersIcon} title={t("团队成员")}>
        <div className="divide-y rounded-lg border">
          {detail.members.map((m) => (
            <div key={m.id} className="flex items-center gap-3 px-3 py-2">
              <UserAvatar user={{ id: m.id, username: m.name, nickname: m.name, email: m.email, avatar: m.avatar }} className="size-7 text-xs" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm">
                  {m.name}
                  {m.isSelf && <span className="text-muted-foreground ml-1 text-xs">({t("我")})</span>}
                </p>
                <p className="text-muted-foreground truncate text-xs">{m.email}</p>
              </div>
              {admin && !m.isSelf && m.role !== "owner" ? (
                <>
                  <Select
                    value={m.role}
                    onValueChange={(role) =>
                      run(`role:${m.id}`, async () => {
                        await api.OrgSetRole(detail.id, m.id, role);
                        await onChanged();
                      })
                    }
                  >
                    <SelectTrigger size="sm" className="w-28">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {ROLES.filter((r) => owner || r !== "admin" || m.role === "admin").map((r) => (
                        <SelectItem key={r} value={r}>
                          {roleLabel(r)}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                  <Button
                    size="icon-sm"
                    variant="ghost"
                    aria-label={t("移出")}
                    onClick={() =>
                      setConfirm({
                        title: t("把 {0} 移出团队?", m.name),
                        body: t("对方之后拉不到团队的新内容, 密钥会自动轮换; 但他本机已同步的副本无法收回, 必要时请更换相关服务器的密码。"),
                        action: t("移出"),
                        run: async () => {
                          await api.OrgRemoveMember(detail.id, m.id);
                          await onChanged();
                        },
                      })
                    }
                  >
                    <TrashIcon />
                  </Button>
                </>
              ) : (
                <span className="text-muted-foreground text-xs">{roleLabel(m.role)}</span>
              )}
            </div>
          ))}
        </div>
      </Section>

      {admin && (
        <Section icon={MailIcon} title={t("邀请成员")}>
          <div className="flex flex-wrap gap-2">
            <Input className="min-w-48 flex-1" type="email" placeholder={t("同事的邮箱")} value={email} onChange={(e) => setEmail(e.target.value)} />
            <RoleSelect value={inviteRole} onChange={setInviteRole} owner={owner} />
            <Button
              disabled={!email.includes("@") || busy === "invite"}
              onClick={() =>
                run("invite", async () => {
                  await api.OrgInviteEmail(detail.id, email.trim(), inviteRole);
                  toast.success(t("邀请已发送到 {0}", email.trim()));
                  setEmail("");
                  await onChanged();
                })
              }
            >
              {busy === "invite" && <Loader2Icon className="animate-spin" />} {t("发送邀请")}
            </Button>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-muted-foreground text-sm">{t("或生成邀请码, 发给同事:")}</span>
            <RoleSelect value={codeRole} onChange={setCodeRole} owner={owner} />
            <Button
              variant="outline"
              disabled={busy === "code"}
              onClick={() =>
                run("code", async () => {
                  setNewCode(await api.OrgCreateInviteCode(detail.id, codeRole, 0, 7));
                  await onChanged();
                })
              }
            >
              <TicketIcon /> {t("生成邀请码")}
            </Button>
          </div>
          {(detail.invites?.length ?? 0) + (detail.inviteCodes?.length ?? 0) > 0 && (
            <div className="divide-y rounded-lg border text-sm">
              {detail.invites?.map((i) => (
                <div key={i.id} className="flex items-center gap-3 px-3 py-2">
                  <MailIcon className="text-muted-foreground size-4" />
                  <span className="min-w-0 flex-1 truncate">
                    {i.email} · {roleLabel(i.role)}
                    <span className="text-muted-foreground"> · {i.status === "pending" ? t("待接受") : i.status === "declined" ? t("已拒绝") : t("已过期")}</span>
                  </span>
                  <Button size="xs" variant="ghost" onClick={() => run(`inv:${i.id}`, async () => (await api.OrgRevokeInvite(detail.id, i.id), await onChanged()))}>
                    {i.status === "pending" ? t("撤回") : t("清除")}
                  </Button>
                </div>
              ))}
              {detail.inviteCodes?.map((c) => (
                <div key={c.id} className="flex items-center gap-3 px-3 py-2">
                  <TicketIcon className="text-muted-foreground size-4" />
                  <span className="min-w-0 flex-1 truncate">
                    {t("邀请码")} · {roleLabel(c.role)}
                    <span className="text-muted-foreground">
                      {" · "}
                      {t("已用 {0} 次", c.usedCount)}
                      {formatDate(c.expiresAt) && ` · ${t("{0} 到期", formatDate(c.expiresAt))}`}
                    </span>
                  </span>
                  <Button size="xs" variant="ghost" onClick={() => run(`code:${c.id}`, async () => (await api.OrgRevokeInviteCode(detail.id, c.id), await onChanged()))}>
                    {t("撤回")}
                  </Button>
                </div>
              ))}
            </div>
          )}
        </Section>
      )}

      <Section icon={LogOutIcon} title={owner ? t("解散团队") : t("退出团队")}>
        <p className="text-muted-foreground text-sm">
          {owner
            ? t("解散后所有成员失去团队空间, 云端数据删除, 本机的团队副本也一并清掉。要保留团队, 可先把所有权转给其他成员。")
            : t("退出后本机的团队副本会清掉, 需要时可请管理员重新邀请。")}
        </p>
        <Button
          size="sm"
          variant="outline"
          className="text-destructive"
          onClick={() =>
            setConfirm({
              title: owner ? t("解散「{0}」?", detail.name) : t("退出「{0}」?", detail.name),
              body: owner ? t("此操作不能撤销。") : t("本机的团队副本会被删除。"),
              action: owner ? t("解散") : t("退出"),
              run: () => leave(false),
            })
          }
        >
          {owner ? t("解散团队") : t("退出团队")}
        </Button>
      </Section>

      <Dialog open={!!newCode} onOpenChange={(o) => !o && setNewCode(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("邀请码已生成")}</DialogTitle>
            <DialogDescription>{t("只显示这一次, 7 天内有效。复制邀请信息发给同事, 对方点链接或输入邀请码即可加入。")}</DialogDescription>
          </DialogHeader>
          <p className="bg-secondary rounded-lg py-4 text-center font-mono text-xl tracking-wider select-all">{newCode?.code}</p>
          <DialogFooter>
            <Button
              onClick={() =>
                newCode &&
                navigator.clipboard.writeText(inviteText(newCode)).then(
                  () => toast.success(t("邀请信息已复制")),
                  () => toast.error(t("复制失败")),
                )
              }
            >
              <CopyIcon /> {t("复制邀请信息")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <PlansDialog
        open={plansOpen}
        onOpenChange={setPlansOpen}
        subject={`org:${detail.id}`}
        plans={plans}
        currentPlan={ent?.entitlement?.active ? ent.entitlement.plan : undefined}
        title={t("「{0}」团队订阅", detail.name)}
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
              variant="destructive"
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

function RoleSelect({ value, onChange, owner }: { value: string; onChange: (v: string) => void; owner: boolean }) {
  useLanguage();
  return (
    <Select value={value} onValueChange={onChange}>
      <SelectTrigger className="w-28">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {ROLES.filter((r) => owner || r !== "admin").map((r) => (
          <SelectItem key={r} value={r}>
            {roleLabel(r)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}

function NameDialog({
  open,
  title,
  label,
  action,
  mono,
  onClose,
  onSubmit,
}: {
  open: boolean;
  title: string;
  label: string;
  action: string;
  mono?: boolean;
  onClose: () => void;
  onSubmit: (v: string) => Promise<void>;
}) {
  useLanguage();
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!value.trim()) return;
    setBusy(true);
    try {
      await onSubmit(value.trim());
      setValue("");
    } catch (err) {
      toast.error(errorText(err));
    } finally {
      setBusy(false);
    }
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-sm">
        <form onSubmit={submit} className="space-y-4">
          <DialogHeader>
            <DialogTitle>{title}</DialogTitle>
          </DialogHeader>
          <Input autoFocus aria-label={label} placeholder={label} className={mono ? "font-mono" : undefined} value={value} onChange={(e) => setValue(e.target.value)} />
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose} disabled={busy}>
              {t("取消")}
            </Button>
            <Button type="submit" disabled={busy || !value.trim()}>
              {busy && <Loader2Icon className="animate-spin" />} {action}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
