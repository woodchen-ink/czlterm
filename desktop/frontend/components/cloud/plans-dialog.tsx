"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useRef, useState } from "react";
import { CheckIcon, ExternalLinkIcon, Loader2Icon, MinusIcon, PlusIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, Events, onEvent, type AddOn, type Plan, type PlanQuote } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatMoney } from "@/lib/format";
import { cn } from "@/lib/utils";

/** 付款等待超过这么久就不再自动检查, 用户仍可手动点「我已完成付款」。 */
const PAY_WAIT_MS = 15 * 60 * 1000;

/** 套餐容量 (GB), 来自 Connect 套餐配置的 quotas.syncStorageGB。 */
export function planStorage(quotas: Record<string, unknown> | undefined): number {
  const v = Number(quotas?.syncStorageGB ?? 0);
  return Number.isFinite(v) ? v : 0;
}

/**
 * 购买 / 续费 / 升级云同步套餐: 选套餐与年限 → 在浏览器打开 Connect 托管结账页 → 本窗口等待到账。
 * 价格取 Connect 下单预览, 前端不算钱; 到账以权益变化为准 (BillingCheckPaid 对比打开前的指纹)。
 */
export function PlansDialog({
  open,
  onOpenChange,
  subject,
  plans,
  currentPlan,
  initialPlan,
  title,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  /** "user" 或 "org:<组织 id>" */
  subject: string;
  plans: Plan[];
  currentPlan?: string;
  /** 打开时预选的套餐 (从套餐卡片点进来时) */
  initialPlan?: string;
  title: string;
}) {
  useLanguage();
  const [plan, setPlan] = useState("");
  const [quote, setQuote] = useState<PlanQuote | null>(null);
  const [quoteError, setQuoteError] = useState("");
  const [periods, setPeriods] = useState(1);
  const [opening, setOpening] = useState(false);
  const pay = usePaymentWait(subject, () => onOpenChange(false));
  const waiting = pay.waiting;

  useEffect(() => {
    if (!open || !plan) return;
    let alive = true;
    queueMicrotask(() => {
      setQuote(null);
      setQuoteError("");
      setPeriods(1);
    });
    api.BillingQuotePlan(plan, subject).then(
      (q) => alive && setQuote(q),
      (e) => alive && setQuoteError(errorText(e)),
    );
    return () => {
      alive = false;
    };
  }, [open, plan, subject]);

  useEffect(() => {
    if (!open) return;
    queueMicrotask(() => {
      pay.reset();
      setPlan(initialPlan || currentPlan || plans[0]?.code || "");
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  async function openCheckout(reuse = false) {
    setOpening(true);
    try {
      pay.start(await api.BillingOpenCheckout(plan, subject, periods), reuse);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setOpening(false);
    }
  }

  const option = quote?.options.find((o) => o.periods === periods) ?? quote?.options[0];
  const selected = plans.find((p) => p.code === plan);

  return (
    <Dialog open={open} onOpenChange={(o) => !opening && onOpenChange(o)}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {waiting
              ? t("已在浏览器打开付款页。付完款回到这里, 会自动确认到账。")
              : t("付款在浏览器里的 CZL Connect 结账页完成, 支持支付宝与微信支付; 优惠券也在那里填写。")}
          </DialogDescription>
        </DialogHeader>

        {waiting ? (
          <div className="flex flex-col items-center gap-3 py-6">
            <Loader2Icon className="text-brand size-8 animate-spin" />
            <p className="text-sm">{t("等待付款完成…")}</p>
            <p className="text-muted-foreground text-xs">{selected && `${selected.name} · ${option ? formatMoney(option.amountCents, quote!.currency) : ""}`}</p>
          </div>
        ) : (
          <div className="space-y-4">
            <div className={cn("grid gap-2", plans.length > 1 && "sm:grid-cols-2")}>
              {plans.map((p) => {
                const gb = planStorage(p.quotas);
                const active = p.code === plan;
                return (
                  <button
                    key={p.code}
                    type="button"
                    onClick={() => setPlan(p.code)}
                    className={cn(
                      "rounded-lg border p-3 text-left transition-colors",
                      active ? "border-brand bg-brand-soft ring-brand/40 ring-1" : "hover:bg-accent/40",
                    )}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className={cn("font-medium", active && "text-brand-strong")}>{p.name}</span>
                      {p.code === currentPlan && <span className="text-muted-foreground text-xs">{t("当前套餐")}</span>}
                    </div>
                    <p className="mt-1 text-lg font-semibold">
                      {formatMoney(p.priceCents, p.currency)}
                      <span className="text-muted-foreground text-xs font-normal"> / {p.periodUnit === "month" ? t("月") : t("年")}</span>
                    </p>
                    <p className="text-muted-foreground mt-1 text-xs">
                      {[p.seats > 1 ? t("{0} 席", p.seats) : "", gb > 0 ? t("{0} GB 存储", gb) : ""].filter(Boolean).join(" · ")}
                    </p>
                  </button>
                );
              })}
            </div>

            {quoteError ? (
              <p className="text-destructive text-sm">{quoteError}</p>
            ) : !quote ? (
              <p className="text-muted-foreground flex items-center gap-2 text-sm">
                <Loader2Icon className="size-4 animate-spin" /> {t("正在计算价格…")}
              </p>
            ) : (
              <div className="space-y-2">
                {quote.options.length > 1 && (
                  <div className="flex flex-wrap gap-1.5">
                    {quote.options.map((o) => (
                      <Button
                        key={o.periods}
                        size="sm"
                        variant={o.periods === periods ? "default" : "outline"}
                        onClick={() => setPeriods(o.periods)}
                      >
                        {t("{0} 年", o.periods)}
                        {o.percentOff > 0 && <span className="opacity-70">−{o.percentOff}%</span>}
                      </Button>
                    ))}
                  </div>
                )}
                {option && (
                  <div className="bg-secondary flex items-baseline justify-between rounded-md px-3 py-2 text-sm">
                    <span className="text-muted-foreground">
                      {quote.kind === "upgrade" ? t("补差价") : t("应付")}
                      {option.savedCents > 0 && (
                        <span className="ml-2 line-through">{formatMoney(option.listCents, quote.currency)}</span>
                      )}
                    </span>
                    <span className="text-lg font-semibold">{formatMoney(option.amountCents, quote.currency)}</span>
                  </div>
                )}
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          {waiting ? (
            <>
              <Button variant="ghost" onClick={() => onOpenChange(false)}>
                {t("取消")}
              </Button>
              <Button variant="outline" onClick={() => openCheckout(true)} disabled={opening}>
                <ExternalLinkIcon /> {t("重新打开付款页")}
              </Button>
              <Button onClick={() => pay.check(true)} disabled={pay.checking}>
                {pay.checking ? <Loader2Icon className="animate-spin" /> : <CheckIcon />} {t("我已完成付款")}
              </Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t("取消")}
              </Button>
              <Button onClick={() => openCheckout()} disabled={!option || opening}>
                {opening ? <Loader2Icon className="animate-spin" /> : <ExternalLinkIcon />} {t("前往付款")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/**
 * 付款等待: 记下打开结账页前的权益指纹, 之后每 4 秒、回到窗口、收到结账页回跳时各查一次,
 * 权益变了即到账。超过 PAY_WAIT_MS 不再自动查, 用户仍可手动确认。
 */
function usePaymentWait(subject: string, onPaid: () => void) {
  const [waiting, setWaiting] = useState<{ fp: string; since: number } | null>(null);
  const [checking, setChecking] = useState(false);
  const doneRef = useRef(false);

  const check = useCallback(
    async (manual: boolean) => {
      if (!waiting || doneRef.current) return;
      setChecking(manual);
      try {
        if (await api.BillingCheckPaid(subject, waiting.fp)) {
          doneRef.current = true;
          toast.success(t("付款成功, 已经生效"));
          onPaid();
        } else if (manual) {
          toast.info(t("还没收到付款结果, 支付平台通知可能有几秒延迟, 稍后会自动刷新"));
        }
      } catch (e) {
        if (manual) toast.error(errorText(e));
      } finally {
        setChecking(false);
      }
    },
    [waiting, subject, onPaid],
  );

  useEffect(() => {
    if (!waiting) return;
    const tick = setInterval(() => {
      if (Date.now() - waiting.since > PAY_WAIT_MS) clearInterval(tick);
      else void check(false);
    }, 4000);
    const onFocus = () => void check(false);
    window.addEventListener("focus", onFocus);
    const off = onEvent(Events.billingDone, () => void check(false));
    return () => {
      clearInterval(tick);
      window.removeEventListener("focus", onFocus);
      off();
    };
  }, [waiting, check]);

  return {
    waiting,
    checking,
    check,
    /** 重新打开付款页时沿用第一次的指纹: 第一次付成功了也能认出来 */
    start: (fp: string, reuse = false) => setWaiting((w) => (reuse && w ? w : { fp, since: Date.now() })),
    reset: () => {
      doneRef.current = false;
      setWaiting(null);
    },
  };
}

/** 加购项每份带来的权益, 如「1 席」「10 GB」。 */
export function addOnUnit(a: { target: string; unitAmount: number }, n = 1): string {
  const v = a.unitAmount * n;
  return a.target === "seats" ? t("{0} 席", v) : t("{0} GB 存储", v);
}

/** 加购席位或容量: 选项与份数 → 浏览器结账页 → 等到账。本期按剩余天数折算, 之后随套餐续费。 */
export function AddOnDialog({
  open,
  onOpenChange,
  subject,
  title,
}: {
  open: boolean;
  onOpenChange: (o: boolean) => void;
  subject: string;
  title: string;
}) {
  useLanguage();
  const [items, setItems] = useState<AddOn[] | null>(null);
  const [error, setError] = useState("");
  const [code, setCode] = useState("");
  const [qty, setQty] = useState(1);
  const [opening, setOpening] = useState(false);
  const pay = usePaymentWait(subject, () => onOpenChange(false));

  useEffect(() => {
    if (!open) return;
    let alive = true;
    queueMicrotask(() => {
      pay.reset();
      setItems(null);
      setError("");
      setQty(1);
    });
    api.BillingAddOns(subject).then(
      (list) => {
        if (!alive) return;
        const usable = (list ?? []).filter((a) => a.remaining > 0 || a.currentQuantity > 0);
        setItems(usable);
        setCode((c) => (usable.some((a) => a.code === c) ? c : (usable.find((a) => a.remaining > 0)?.code ?? "")));
      },
      (e) => alive && setError(errorText(e)),
    );
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, subject]);

  const item = items?.find((a) => a.code === code);
  const max = item?.remaining ?? 0;
  const n = Math.min(Math.max(qty, 1), Math.max(max, 1));

  async function openCheckout(reuse = false) {
    if (!item) return;
    setOpening(true);
    try {
      pay.start(await api.BillingOpenAddOnCheckout(item.code, subject, n), reuse);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setOpening(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !opening && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>
            {pay.waiting
              ? t("已在浏览器打开付款页。付完款回到这里, 会自动确认到账。")
              : t("本期按剩余天数折算, 之后随套餐一起续费。")}
          </DialogDescription>
        </DialogHeader>

        {pay.waiting ? (
          <div className="flex flex-col items-center gap-3 py-6">
            <Loader2Icon className="text-brand size-8 animate-spin" />
            <p className="text-sm">{t("等待付款完成…")}</p>
            <p className="text-muted-foreground text-xs">{item && `${item.name} × ${n}`}</p>
          </div>
        ) : error ? (
          <p className="text-destructive text-sm">{error}</p>
        ) : !items ? (
          <p className="text-muted-foreground flex items-center gap-2 text-sm">
            <Loader2Icon className="size-4 animate-spin" /> {t("正在加载…")}
          </p>
        ) : items.length === 0 ? (
          <p className="text-muted-foreground text-sm">{t("当前套餐没有可加购的项目。")}</p>
        ) : (
          <div className="space-y-3">
            <div className="grid gap-2 sm:grid-cols-2">
              {items.map((a) => {
                const active = a.code === code;
                return (
                  <button
                    key={a.code}
                    type="button"
                    disabled={a.remaining <= 0}
                    onClick={() => {
                      setCode(a.code);
                      setQty(1);
                    }}
                    className={cn(
                      "rounded-lg border p-3 text-left transition-colors disabled:opacity-50",
                      active ? "border-brand bg-brand-soft ring-brand/40 ring-1" : "hover:bg-accent/40",
                    )}
                  >
                    <p className={cn("font-medium", active && "text-brand-strong")}>{a.name}</p>
                    <p className="text-muted-foreground mt-1 text-xs">
                      {t("{0} / 份 · 每份 {1}", formatMoney(a.priceCents, a.currency), addOnUnit(a))}
                    </p>
                    <p className="text-muted-foreground text-xs">
                      {a.remaining > 0 ? t("已有 {0} 份, 还能加 {1} 份", a.currentQuantity, a.remaining) : t("已达上限")}
                    </p>
                  </button>
                );
              })}
            </div>
            {item && max > 0 && (
              <div className="bg-secondary flex items-center gap-3 rounded-md px-3 py-2 text-sm">
                <div className="flex items-center gap-1">
                  <Button size="icon-sm" variant="outline" aria-label={t("减少")} disabled={n <= 1} onClick={() => setQty(n - 1)}>
                    <MinusIcon />
                  </Button>
                  <span className="w-8 text-center font-medium tabular-nums">{n}</span>
                  <Button size="icon-sm" variant="outline" aria-label={t("增加")} disabled={n >= max} onClick={() => setQty(n + 1)}>
                    <PlusIcon />
                  </Button>
                </div>
                <span className="text-muted-foreground flex-1">+{addOnUnit(item, n)}</span>
                <span className="text-lg font-semibold">{formatMoney(item.proratedUnitCents * n, item.currency)}</span>
              </div>
            )}
          </div>
        )}

        <DialogFooter>
          {pay.waiting ? (
            <>
              <Button variant="ghost" onClick={() => onOpenChange(false)}>
                {t("取消")}
              </Button>
              <Button variant="outline" onClick={() => openCheckout(true)} disabled={opening}>
                <ExternalLinkIcon /> {t("重新打开付款页")}
              </Button>
              <Button onClick={() => pay.check(true)} disabled={pay.checking}>
                {pay.checking ? <Loader2Icon className="animate-spin" /> : <CheckIcon />} {t("我已完成付款")}
              </Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={() => onOpenChange(false)}>
                {t("取消")}
              </Button>
              <Button onClick={() => openCheckout()} disabled={!item || max <= 0 || opening}>
                {opening ? <Loader2Icon className="animate-spin" /> : <ExternalLinkIcon />} {t("前往付款")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
