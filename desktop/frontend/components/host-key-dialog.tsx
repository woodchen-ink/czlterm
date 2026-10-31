"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useEffect, useState } from "react";
import { toast } from "sonner";

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
import { api, Events, onEvent, type HostKeyChange } from "@/lib/api";
import { errorText } from "@/lib/errors";

/** 主机密钥与 known_hosts 不符时弹出, 用户核实指纹后替换记录。同一主机的新提示覆盖旧提示。 */
export function HostKeyDialog() {
  useLanguage();
  const [change, setChange] = useState<HostKeyChange | null>(null);

  useEffect(() => onEvent<HostKeyChange>(Events.hostKeyChanged, setChange), []);

  async function trust() {
    const c = change;
    setChange(null);
    if (!c) return;
    try {
      await api.TrustHostKey(c.host, c.fingerprint);
      toast.success(t("已更新 {0} 的主机密钥, 请重新连接", c.host));
    } catch (e) {
      toast.error(errorText(e));
    }
  }

  return (
    <AlertDialog open={!!change} onOpenChange={(o) => !o && setChange(null)}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{t("{0} 的主机密钥变了", change?.host)}</AlertDialogTitle>
          <AlertDialogDescription>
            {t("服务器出示的主机密钥与 known_hosts 里的记录不同。重装系统或重新生成主机密钥后会这样; 如果服务器没有做过这些操作, 可能有人在中间拦截连接, 请先通过其他途径核实指纹。")}
          </AlertDialogDescription>
        </AlertDialogHeader>
        {change && (
          <div className="space-y-2 text-sm">
            <div>
              <div className="text-muted-foreground text-xs">{t("新指纹 ({0})", change.keyType)}</div>
              <div className="font-mono text-xs break-all">{change.fingerprint}</div>
            </div>
            <div>
              <div className="text-muted-foreground text-xs">{t("原记录")}</div>
              {(change.known ?? []).map((fp) => (
                <div key={fp} className="text-muted-foreground font-mono text-xs break-all">
                  {fp}
                </div>
              ))}
            </div>
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
          <AlertDialogAction onClick={trust}>{t("信任新密钥")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
