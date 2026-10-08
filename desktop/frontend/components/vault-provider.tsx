"use client";

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
  const [status, setStatus] = useState<VaultStatus | null>(null);
  const [open, setOpen] = useState(false);
  // 同一时间可能有多个操作在等解锁, 解锁一次后全部继续。
  const waiters = useRef<((ok: boolean) => void)[]>([]);

  const refreshStatus = useCallback(async () => {
    try {
      setStatus(await api.VaultStatus());
    } catch {
      // 状态查询失败不打断界面, 下次操作时再报。
    }
  }, []);

  useEffect(() => {
    api
      .VaultStatus()
      .then(setStatus)
      .catch(() => {});
    return onEvent(Events.vaultLocked, () => {
      void refreshStatus();
      toast.info("保险库已锁定");
    });
  }, [refreshStatus]);

  const settle = useCallback((ok: boolean) => {
    const list = waiters.current;
    waiters.current = [];
    list.forEach((w) => w(ok));
  }, []);

  const requestUnlock = useCallback(() => {
    // 状态可能已过期 (例如刚在终端里登录了 bw), 打开前刷新一次。
    void refreshStatus();
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
        onUnlocked={async () => {
          await refreshStatus();
          setOpen(false);
          settle(true);
        }}
      />
    </VaultContext.Provider>
  );
}
