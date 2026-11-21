"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { createContext, useCallback, useContext, useRef, useState } from "react";
import { ArrowRightIcon, Loader2Icon, ShieldQuestionIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api, type ConnectionReview } from "@/lib/api";
import { errorText, parseError } from "@/lib/errors";

/** 用户在确认框里取消了: 调用方应静默放弃, 不再弹错误。 */
export class ConfirmCancelled extends Error {}

type Gate = <T>(fn: () => Promise<T>) => Promise<T>;

const Ctx = createContext<Gate | null>(null);

/**
 * 团队连接确认: 别人新建或改过主机 / 凭据引用的团队连接, 后端返回 6416, 这里弹框列出
 * 「会把凭据交给哪台机器」, 用户确认后记下并重试一次原操作。
 */
export function useConfirmGate(): Gate {
  const g = useContext(Ctx);
  if (!g) throw new Error("useConfirmGate outside ConfirmConnectionProvider");
  return g;
}

export function ConfirmConnectionProvider({ children }: { children: React.ReactNode }) {
  useLanguage();
  const [review, setReview] = useState<ConnectionReview[] | null>(null);
  const [busy, setBusy] = useState(false);
  const pending = useRef<{ id: string; resolve: (ok: boolean) => void } | null>(null);

  const ask = useCallback(async (id: string) => {
    let items: ConnectionReview[] = [];
    try {
      items = await api.ReviewConnection(id);
    } catch {
      /* 连接已不存在: 照样问, 列表空着 */
    }
    setReview(items);
    return new Promise<boolean>((resolve) => {
      pending.current = { id, resolve };
    });
  }, []);

  const gate = useCallback<Gate>(
    async (fn) => {
      try {
        return await fn();
      } catch (e) {
        const { code, raw } = parseError(e);
        if (code !== "6416") throw e;
        const id = raw.split(":")[1] ?? "";
        if (!(await ask(id))) throw new ConfirmCancelled();
        return await fn();
      }
    },
    [ask],
  );

  function finish(ok: boolean) {
    const p = pending.current;
    pending.current = null;
    setReview(null);
    p?.resolve(ok);
  }

  async function confirm() {
    const p = pending.current;
    if (!p) return;
    setBusy(true);
    try {
      await api.ConfirmConnection(p.id);
      finish(true);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Ctx.Provider value={gate}>
      {children}
      <Dialog open={!!review} onOpenChange={(o) => !o && !busy && finish(false)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ShieldQuestionIcon className="text-warning size-4" /> {t("确认这条团队连接")}
            </DialogTitle>
            <DialogDescription>
              {t("这条连接的地址或凭据是同事设置或修改的。连接时会把凭据交给下面的机器, 确认地址无误再继续。")}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-1.5">
            {review?.map((r, i) => (
              <div key={r.id} className="bg-secondary rounded-md px-3 py-2 text-sm">
                <p className="flex items-center gap-1.5 font-medium">
                  {i > 0 && <ArrowRightIcon className="text-muted-foreground size-3.5" />}
                  {r.name}
                  {i > 0 && <span className="text-muted-foreground text-xs font-normal">({t("跳板机")})</span>}
                </p>
                <p className="font-mono text-xs break-all">
                  {r.protocol}://{r.username ? `${r.username}@` : ""}
                  {r.host}:{r.port}
                </p>
                {r.credential && <p className="text-muted-foreground text-xs">{t("凭据: {0}", r.credential)}</p>}
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => finish(false)} disabled={busy}>
              {t("取消")}
            </Button>
            <Button onClick={() => void confirm()} disabled={busy}>
              {busy && <Loader2Icon className="animate-spin" />} {t("确认并连接")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </Ctx.Provider>
  );
}
