"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useState } from "react";
import { KeyRoundIcon, Loader2Icon, PencilIcon, PlusIcon, TrashIcon } from "lucide-react";
import { toast } from "sonner";

import { spaceLabel, useCloud } from "@/components/cloud/cloud-provider";
import { CredentialDialog } from "@/components/credential-dialog";
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
import { api, type CredentialList, type CredentialSummary } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatDate } from "@/lib/format";

/** 设置 → 凭据: 当前空间里保存的凭据 (不用 Vaultwarden 时的密码与私钥)。只显示摘要, 秘密不回显。 */
export function CredentialsPanel() {
  useLanguage();
  const { current } = useCloud();
  const [list, setList] = useState<CredentialList | null>(null);
  const [editing, setEditing] = useState<CredentialSummary | "new" | null>(null);
  const [removing, setRemoving] = useState<CredentialSummary | null>(null);

  const load = useCallback(() => {
    api.ListCredentials().then(setList, (e) => toast.error(errorText(e)));
  }, []);
  useEffect(() => {
    queueMicrotask(load);
  }, [load, current.key]);

  if (!list) {
    return (
      <div className="text-muted-foreground flex items-center justify-center py-16">
        <Loader2Icon className="animate-spin" />
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div className="flex items-start gap-3">
        <div className="min-w-0 flex-1">
          <p className="text-sm">{t("「{0}」里的凭据", spaceLabel(current))}</p>
          <p className="text-muted-foreground text-xs">
            {list.shared
              ? t("团队空间: 成员都能使用这里的凭据。")
              : t("不用 Vaultwarden 时, 连接的密码与私钥存在这里, 加密保存; 开启云同步后随空间端到端加密同步。")}
          </p>
        </div>
        <Button size="sm" onClick={() => setEditing("new")} disabled={list.readOnly}>
          <PlusIcon /> {t("新建凭据")}
        </Button>
      </div>

      {list.warnings.map((w) => (
        <p key={w} className="text-destructive text-xs">
          {errorText(w)}
        </p>
      ))}

      {list.items.length === 0 ? (
        <div className="text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm">
          <KeyRoundIcon className="mx-auto mb-2 size-6" />
          {t("还没有保存的凭据。在连接里选「保存的凭据」也能直接新建。")}
        </div>
      ) : (
        <div className="divide-y rounded-lg border">
          {list.items.map((c) => (
            <div key={c.id} className="flex items-center gap-3 px-3 py-2.5">
              <KeyRoundIcon className="text-muted-foreground size-4 shrink-0" />
              <div className="min-w-0 flex-1">
                <p className="truncate text-sm font-medium">{c.name}</p>
                <p className="text-muted-foreground truncate text-xs">
                  {[c.username, c.hasPassword ? t("密码") : "", c.hasKey ? t("私钥") : "", formatDate(c.updatedAt)].filter(Boolean).join(" · ")}
                </p>
              </div>
              <Button size="icon-sm" variant="ghost" aria-label={t("编辑")} disabled={list.readOnly} onClick={() => setEditing(c)}>
                <PencilIcon />
              </Button>
              <Button size="icon-sm" variant="ghost" aria-label={t("删除")} disabled={list.readOnly} onClick={() => setRemoving(c)}>
                <TrashIcon />
              </Button>
            </div>
          ))}
        </div>
      )}

      {editing && (
        <CredentialDialog
          open
          initial={editing === "new" ? null : editing}
          shared={list.shared}
          onOpenChange={(o) => !o && setEditing(null)}
          onSaved={() => {
            setEditing(null);
            load();
          }}
        />
      )}

      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("删除凭据 {0}?", removing?.name)}</AlertDialogTitle>
            <AlertDialogDescription>
              {list.shared ? t("会从团队空间里删除, 其他成员也用不了了。") : t("删除后无法恢复。还被连接使用的凭据不能删除。")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("取消")}</AlertDialogCancel>
            <AlertDialogAction
              variant="destructive"
              onClick={() => {
                const c = removing;
                setRemoving(null);
                if (!c) return;
                api.DeleteCredential(c.id).then(
                  () => {
                    toast.success(t("凭据已删除"));
                    load();
                  },
                  (e) => toast.error(errorText(e)),
                );
              }}
            >
              {t("删除")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
