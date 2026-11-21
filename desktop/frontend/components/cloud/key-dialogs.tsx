"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useEffect, useState } from "react";
import { CheckIcon, CopyIcon, DownloadIcon, KeyRoundIcon, LaptopIcon, Loader2Icon, MonitorSmartphoneIcon, ShieldCheckIcon } from "lucide-react";
import { toast } from "sonner";

import { FileTextInput } from "@/components/file-text-input";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { api, Events, onEvent, type DeviceProgress, type DeviceRequest } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { cn } from "@/lib/utils";

/** 从粘贴或文件内容里找出恢复码 (8 组 4 位, 分隔符可以是连字符或空格)。 */
export function extractRecoveryCode(text: string): string | null {
  const m = text.match(/[0-9A-Za-z]{4}(?:[-\s]+[0-9A-Za-z]{4}){7}/);
  return m ? m[0].replace(/[-\s]+/g, "-").toUpperCase() : null;
}

/**
 * 开通或重置后展示恢复码: 只显示这一次。复制或另存成文件后勾选「已保存」才能关闭,
 * 不能点外面或按 Esc 关掉。
 */
export function RecoveryCodeDialog({ code, onDone }: { code: string; onDone: () => void }) {
  useLanguage();
  const [saved, setSaved] = useState(false);
  const [confirmed, setConfirmed] = useState(false);

  function copy() {
    navigator.clipboard.writeText(code).then(
      () => {
        setSaved(true);
        toast.success(t("恢复码已复制, 请粘贴到安全的地方保存"));
      },
      () => toast.error(t("复制失败")),
    );
  }

  async function saveFile() {
    try {
      if (await api.SaveRecoveryCode(code)) {
        setSaved(true);
        toast.success(t("恢复码已保存为文件"));
      }
    } catch (e) {
      toast.error(errorText(e));
    }
  }

  return (
    <Dialog open={!!code}>
      <DialogContent
        className="sm:max-w-lg"
        showCloseButton={false}
        onEscapeKeyDown={(e) => e.preventDefault()}
        onPointerDownOutside={(e) => e.preventDefault()}
      >
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <ShieldCheckIcon className="size-4 text-emerald-600" /> {t("保存你的恢复码")}
          </DialogTitle>
          <DialogDescription>
            {t("云同步是端到端加密的: 只有你的设备能解开数据, CZL 也看不到。换电脑时需要恢复码 (或一台已登录的设备) 才能解锁。")}
          </DialogDescription>
        </DialogHeader>
        <div className="bg-secondary rounded-lg px-4 py-5 text-center font-mono text-lg tracking-wider break-all select-all">{code}</div>
        <div className="grid grid-cols-2 gap-2">
          <Button variant="outline" onClick={copy}>
            <CopyIcon /> {t("复制")}
          </Button>
          <Button variant="outline" onClick={saveFile}>
            <DownloadIcon /> {t("另存为文件")}
          </Button>
        </div>
        <p className="text-muted-foreground text-xs">
          {t("建议存进密码管理器, 比如在 Vaultwarden / Bitwarden 里新建一条安全笔记。")}
        </p>
        <p className="text-destructive text-xs">
          {t("恢复码只显示这一次。丢了它、又没有其它已登录的设备时, 云端数据无法恢复, 客服也无法代为找回。")}
        </p>
        <label className={cn("flex items-start gap-2 text-sm", !saved && "opacity-60")}>
          <Checkbox checked={confirmed} disabled={!saved} onCheckedChange={(v) => setConfirmed(v === true)} className="mt-0.5" />
          <span>{saved ? t("我已把恢复码保存在安全的地方") : t("先复制或另存为文件, 再勾选确认")}</span>
        </label>
        <DialogFooter>
          <Button disabled={!confirmed} onClick={onDone}>
            <CheckIcon /> {t("完成")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

/** 新电脑解锁: 输入恢复码 (可粘贴、拖入或选择恢复码文件), 或请已登录的设备批准。 */
export function UnlockDialog({ open, onOpenChange, onUnlocked }: { open: boolean; onOpenChange: (o: boolean) => void; onUnlocked: () => void }) {
  useLanguage();
  const [tab, setTab] = useState("code");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [progress, setProgress] = useState<DeviceProgress | null>(null);
  const [waiting, setWaiting] = useState(false);

  useEffect(() => {
    if (!open) return;
    return onEvent<DeviceProgress>(Events.cloudDevice, (p) => {
      setProgress(p);
      if (p.stage !== "code") setWaiting(false);
      if (p.stage === "approved") {
        toast.success(t("已获批准, 本机已解锁"));
        onUnlocked();
      }
    });
  }, [open, onUnlocked]);

  function close(o: boolean) {
    if (!o && waiting) void api.CloudCancelDeviceEnrollment();
    if (!o) {
      setCode("");
      setProgress(null);
      setWaiting(false);
    }
    onOpenChange(o);
  }

  async function unlock() {
    setBusy(true);
    try {
      await api.CloudUnlock(code);
      toast.success(t("已解锁, 正在同步"));
      setCode("");
      onUnlocked();
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  async function requestApproval() {
    setProgress(null);
    setWaiting(true);
    try {
      await api.CloudStartDeviceEnrollment();
    } catch (e) {
      setWaiting(false);
      toast.error(errorText(e));
    }
  }

  const valid = !!extractRecoveryCode(code);

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <KeyRoundIcon className="size-4" /> {t("在这台电脑上解锁云同步")}
          </DialogTitle>
          <DialogDescription>{t("这个账号已在别的设备开通过端到端加密, 本机还没有解密密钥。任选一种方式解锁:")}</DialogDescription>
        </DialogHeader>
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList className="w-full">
            <TabsTrigger value="code">{t("恢复码")}</TabsTrigger>
            <TabsTrigger value="device">{t("已登录的设备批准")}</TabsTrigger>
          </TabsList>
          <TabsContent value="code" className="space-y-3 pt-2">
            <FileTextInput
              value={code}
              onChange={setCode}
              autoFocus
              rows={2}
              extract={extractRecoveryCode}
              invalidHint={t("没有找到恢复码: 应是 8 组、每组 4 位的字母数字")}
              placeholder="XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX-XXXX"
            />
            <DialogFooter>
              <Button onClick={unlock} disabled={!valid || busy}>
                {busy && <Loader2Icon className="animate-spin" />} {t("解锁")}
              </Button>
            </DialogFooter>
          </TabsContent>
          <TabsContent value="device" className="space-y-3 pt-2">
            <p className="text-muted-foreground text-sm">
              {t("在另一台已解锁的电脑上打开 czlterm 设置 → 账号与同步, 那里会出现本机的批准请求。两边显示同一个 6 位码时再批准。")}
            </p>
            {progress?.stage === "code" ? (
              <div className="space-y-2 text-center">
                <p className="text-muted-foreground text-xs">{t("核对码")}</p>
                <p className="font-mono text-3xl tracking-[0.3em]">{progress.code}</p>
                <p className="text-muted-foreground flex items-center justify-center gap-2 text-xs">
                  <Loader2Icon className="size-3 animate-spin" /> {t("等待那台设备批准…")}
                </p>
              </div>
            ) : waiting ? (
              <p className="text-muted-foreground flex items-center justify-center gap-2 py-4 text-sm">
                <Loader2Icon className="size-4 animate-spin" /> {t("已发出请求, 等待已登录的设备响应 (10 分钟内有效)…")}
              </p>
            ) : (
              progress?.stage === "failed" && <p className="text-destructive text-sm">{errorText(progress.error)}</p>
            )}
            <DialogFooter>
              {waiting ? (
                <Button variant="outline" onClick={() => close(false)}>
                  {t("撤回请求")}
                </Button>
              ) : (
                <Button onClick={requestApproval}>
                  <MonitorSmartphoneIcon /> {progress?.stage === "failed" ? t("重新请求") : t("请求批准")}
                </Button>
              )}
            </DialogFooter>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  );
}

/** 已解锁的设备批准一台新设备: 先取 6 位码给用户核对, 一致才批准。 */
export function ApproveDeviceDialog({ request, onClose }: { request: DeviceRequest | null; onClose: () => void }) {
  useLanguage();
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!request) return;
    let alive = true;
    queueMicrotask(() => {
      setCode("");
      setError("");
    });
    api.CloudBeginDeviceApproval(request.id).then(
      (c) => alive && setCode(c),
      (e) => alive && setError(errorText(e)),
    );
    return () => {
      alive = false;
    };
  }, [request]);

  async function finish(approve: boolean) {
    if (!request) return;
    setBusy(true);
    try {
      await api.CloudFinishDeviceApproval(request.id, approve);
      toast.success(approve ? t("已批准 {0}", request.deviceName) : t("已拒绝 {0}", request.deviceName));
      onClose();
    } catch (e) {
      toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Dialog open={!!request} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <LaptopIcon className="size-4" /> {t("批准新设备")}
          </DialogTitle>
          <DialogDescription>
            {t("「{0}」请求解锁你的云同步。只有那台电脑是你本人在操作、且两边显示同一个码时才批准。", request?.deviceName ?? "")}
          </DialogDescription>
        </DialogHeader>
        <div className="py-2 text-center">
          {error ? (
            <p className="text-destructive text-sm">{error}</p>
          ) : code ? (
            <p className="font-mono text-3xl tracking-[0.3em]">{code}</p>
          ) : (
            <p className="text-muted-foreground flex items-center justify-center gap-2 text-sm">
              <Loader2Icon className="size-4 animate-spin" /> {t("正在与那台设备交换核对码…")}
            </p>
          )}
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => finish(false)} disabled={busy || !code}>
            {t("不一致, 拒绝")}
          </Button>
          <Button onClick={() => finish(true)} disabled={busy || !code}>
            {busy && <Loader2Icon className="animate-spin" />} {t("一致, 批准")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
