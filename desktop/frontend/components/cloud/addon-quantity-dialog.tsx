"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useEffect, useState } from "react";
import { AlertTriangleIcon, Loader2Icon, MinusIcon, PlusIcon } from "lucide-react";
import { toast } from "sonner";

import { addOnUnit } from "@/components/cloud/plans-dialog";
import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, type Entitlement } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatDate } from "@/lib/format";

/** 加购项下期份数: 有调整取调整后的, 否则沿用当前份数。 */
export function nextQuantity(a: { quantity: number; pendingQuantity?: number }): number {
  return a.pendingQuantity ?? a.quantity;
}

/**
 * 调低加购项在下个周期的份数 (Connect 只允许调低, 当前周期结束后生效; 调回当前份数即撤回)。
 * 增加份数走加购结账 (AddOnDialog)。保存后 Go 端刷新订阅状态, 界面经 billing:changed 更新。
 */
export function AddOnQuantityDialog({
  open,
  onOpenChange,
  subject,
  title,
  ent,
  members,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  /** "user" 或 "org:<组织 id>" */
  subject: string;
  title: string;
  ent?: Entitlement;
  /** 团队当前成员数, 用于提示下期席位不够 */
  members?: number;
}) {
  useLanguage();
  const addons = (ent?.addons ?? []).filter((a) => a.quantity > 0);
  const [next, setNext] = useState<Record<string, number>>({});
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    if (!open) return;
    queueMicrotask(() => setNext(Object.fromEntries((ent?.addons ?? []).map((a) => [a.code, nextQuantity(a)]))));
    // 只在打开时取初值, 保存后的刷新不覆盖正在编辑的份数
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const changed = addons.filter((a) => next[a.code] !== undefined && next[a.code] !== nextQuantity(a));
  // 下期席位 = 本期合计 − 席位加购减少的部分
  const nextSeats =
    (ent?.seats ?? 0) -
    addons.filter((a) => a.target === "seats").reduce((s, a) => s + (a.quantity - (next[a.code] ?? nextQuantity(a))) * a.unitAmount, 0);
  const seatsShort = members !== undefined && nextSeats < members;

  async function save() {
    setSaving(true);
    try {
      for (const a of changed) await api.BillingSetAddOnQuantity(subject, a.code, next[a.code]);
      toast.success(t("已保存, {0} 到期后按新份数续费", formatDate(ent?.periodEnd)));
      onOpenChange(false);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setSaving(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !saving && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {t("调低的份数在 {0} 到期后生效, 本期照常使用、已付的不退; 续费按调整后的份数计费。要增加请用「继续加购」。", formatDate(ent?.periodEnd))}
          </DialogDescription>
        </DialogHeader>

        {addons.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t("还没有加购项目。")}</p>
        ) : (
          <div className="divide-y rounded-lg border">
            {addons.map((a) => {
              const n = next[a.code] ?? nextQuantity(a);
              return (
                <div key={a.code} className="flex items-center gap-3 px-3 py-2.5">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium">{a.name}</p>
                    <p className="text-muted-foreground truncate text-xs">
                      {t("本期 {0} 份 ({1})", a.quantity, addOnUnit(a, a.quantity))}
                      {n !== a.quantity && ` · ${n > 0 ? t("下期 {0}", addOnUnit(a, n)) : t("下期不再续")}`}
                    </p>
                  </div>
                  <div className="flex items-center gap-1">
                    <Button
                      size="icon-sm"
                      variant="outline"
                      aria-label={t("减少")}
                      disabled={saving || n <= 0}
                      onClick={() => setNext((m) => ({ ...m, [a.code]: n - 1 }))}
                    >
                      <MinusIcon />
                    </Button>
                    <span className="w-8 text-center text-sm font-medium tabular-nums">{n}</span>
                    <Button
                      size="icon-sm"
                      variant="outline"
                      aria-label={t("增加")}
                      disabled={saving || n >= a.quantity}
                      onClick={() => setNext((m) => ({ ...m, [a.code]: n + 1 }))}
                    >
                      <PlusIcon />
                    </Button>
                  </div>
                </div>
              );
            })}
          </div>
        )}

        {seatsShort && (
          <p className="text-warning flex items-start gap-1.5 text-xs">
            <AlertTriangleIcon className="mt-px size-3.5 shrink-0" />
            {t("下期只有 {0} 席, 少于现在的 {1} 位成员; 到期前不移除成员, 团队空间会变为只读。", nextSeats, members)}
          </p>
        )}

        <DialogFooter>
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={saving}>
            {t("取消")}
          </Button>
          <Button onClick={save} disabled={saving || changed.length === 0}>
            {saving && <Loader2Icon className="animate-spin" />} {t("保存")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
