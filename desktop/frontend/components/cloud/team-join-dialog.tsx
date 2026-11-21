"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useState } from "react";
import { Loader2Icon, UsersIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { api } from "@/lib/api";
import { errorText } from "@/lib/errors";

/** 经 czlterm://team/join 链接送来的邀请: 用户确认后才加入。未登录时提示先登录, 登录后会再弹出。 */
export function TeamJoinDialog({
  code,
  loggedIn,
  onClose,
  onJoined,
}: {
  code: string;
  loggedIn: boolean;
  onClose: () => void;
  onJoined: (orgId: string) => void;
}) {
  useLanguage();
  const [busy, setBusy] = useState(false);
  const open = !!code && loggedIn;

  async function join() {
    setBusy(true);
    try {
      const r = await api.OrgJoin(code);
      toast.success(r.alreadyMember ? t("你已经在团队「{0}」里", r.org.name) : t("已加入团队「{0}」", r.org.name));
      onJoined(r.org.id);
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <UsersIcon className="size-4" /> {t("加入团队")}
          </DialogTitle>
          <DialogDescription>
            {t("收到一个团队邀请。加入后, 团队共享的连接与脚本会出现在空间切换里; 团队里的内容同样端到端加密。")}
          </DialogDescription>
        </DialogHeader>
        <p className="bg-secondary rounded-md px-3 py-2 font-mono text-sm">{code}</p>
        <DialogFooter>
          <Button variant="outline" onClick={onClose} disabled={busy}>
            {t("取消")}
          </Button>
          <Button onClick={join} disabled={busy}>
            {busy && <Loader2Icon className="animate-spin" />} {t("加入")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
