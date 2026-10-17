"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { createContext, useCallback, useContext, useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { UnlockDialog } from "@/components/unlock-dialog";
import { api, Events, onEvent, type VaultStatus } from "@/lib/api";
import { isLockedError } from "@/lib/errors";

interface VaultContextValue {
  status: VaultStatus | null;
  refreshStatus: () => Promise<void>;
  /** 打开解锁框, 解锁成功时 resolve, 用户取消时 resolve(false)。 */
  requestUnlock: () => Promise<boolean>;
  /** 执行一个可能需要保险库的操作: 遇到"已锁定"时弹出解锁框, 解锁后重试一次。 */
  withVault: <T>(fn: () => Promise<T>) => Promise<T>;
  lock: () => Promise<void>;
}

const VaultContext = createContext<VaultContextValue | null>(null);

export function useVault(): VaultContextValue {
  const v = useContext(VaultContext);
  if (!v) throw new Error("useVault outside VaultProvider");
  return v;
}

/** 取消解锁时抛出, 调用方据此静默结束而不弹错误。 */
export class UnlockCancelled extends Error {
  constructor() {
    super("unlock cancelled");
  }
}

export function VaultProvider({ children }: { children: React.ReactNode }) {
  useLanguage();
  const [status, setStatus] = useState<VaultStatus | null>(null);
  const [open, setOpen] = useState(false);
  // 同一时间可能有多个操作在等解锁, 解锁一次后全部继续。
  const waiters = useRef<((ok: boolean) => void)[]>([]);
  const statusRequest = useRef(0);
  const statusPending = useRef(false);
  const latestStatus = useRef<VaultStatus | null>(null);
  const lifecycle = useRef({ active: false, generation: 0 });

  /** 一次解锁或取消, 结清所有正在等待的操作。 */
  const settle = useCallback((ok: boolean) => {
    const list = waiters.current;
    waiters.current = [];
    list.forEach((w) => w(ok));
  }, []);

  /** 只应用最新一次查询, 避免启动查询的旧结果覆盖后台恢复后的状态。 */
  const refreshStatus = useCallback(async () => {
    if (!lifecycle.current.active) return;
    const request = ++statusRequest.current;
    statusPending.current = true;
    try {
      const next = await api.VaultStatus();
      if (request !== statusRequest.current) return;
      latestStatus.current = next;
      setStatus(next);
      if (next.status === "unlocked") {
        setOpen(false);
        settle(true);
      }
    } catch {
      // 状态查询失败不打断界面, 下次复查或操作时重试。
    } finally {
      if (request === statusRequest.current) statusPending.current = false;
    }
  }, [settle]);

  useEffect(() => {
    let active = true;
    const requests = statusRequest;
    const pending = statusPending;
    const lifetime = lifecycle.current;
    lifetime.active = true;
    lifetime.generation++;
    // 先订阅再查询, 防止初始化期间漏掉后台恢复事件。
    const offLocked = onEvent(Events.vaultLocked, () => {
      void refreshStatus();
      toast.info(t("保险库已锁定"));
    });
    const offUnlocked = onEvent(Events.vaultUnlocked, () => void refreshStatus());
    /** 窗口恢复时重新核对, 不主动恢复已被用户锁定的会话。 */
    const handleVisible = () => {
      if (document.visibilityState === "visible") void refreshStatus();
    };
    window.addEventListener("focus", handleVisible);
    document.addEventListener("visibilitychange", handleVisible);
    // 未解锁时兜底复查; 隐藏窗口、已解锁或仍有查询时不反复启动 bw。
    const timer = window.setInterval(() => {
      if (document.visibilityState === "visible" &&
          latestStatus.current?.status !== "unlocked" && !statusPending.current) {
        void refreshStatus();
      }
    }, 5000);
    queueMicrotask(() => {
      if (active) void refreshStatus();
    });
    return () => {
      active = false;
      offLocked();
      offUnlocked();
      window.removeEventListener("focus", handleVisible);
      document.removeEventListener("visibilitychange", handleVisible);
      window.clearInterval(timer);
      requests.current++;
      pending.current = false;
      lifetime.active = false;
      lifetime.generation++;
      settle(false);
    };
  }, [refreshStatus, settle]);

  const requestUnlock = useCallback(async () => {
    // 自动恢复可能已经完成, 先核对再决定是否需要输入主密码。
    const generation = lifecycle.current.generation;
    await refreshStatus();
    if (!lifecycle.current.active || generation !== lifecycle.current.generation) return false;
    if (latestStatus.current?.status === "unlocked") return true;
    setOpen(true);
    return new Promise<boolean>((resolve) => waiters.current.push(resolve));
  }, [refreshStatus]);

  const withVault = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T> => {
      try {
        return await fn();
      } catch (e) {
        if (!isLockedError(e)) throw e;
        if (!(await requestUnlock())) throw new UnlockCancelled();
        return fn();
      }
    },
    [requestUnlock],
  );

  const lock = useCallback(async () => {
    await api.VaultLock();
    await refreshStatus();
  }, [refreshStatus]);

  return (
    <VaultContext.Provider value={{ status, refreshStatus, requestUnlock, withVault, lock }}>
      {children}
      <UnlockDialog
        open={open}
        status={status}
        onOpenChange={(o) => {
          setOpen(o);
          if (!o) settle(false);
        }}
        onUnlocked={refreshStatus}
      />
    </VaultContext.Provider>
  );
}
