"use client";

import { t } from "@/lib/i18n";

import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState } from "react";
import { toast } from "sonner";

import { TeamJoinDialog } from "@/components/cloud/team-join-dialog";
import {
  api,
  Events,
  LOCAL_SPACE,
  onEvent,
  type AccountStatus,
  type AccountUser,
  type BillingState,
  type CloudState,
  type ConflictInfo,
  type SpacesView,
  type SpaceView,
} from "@/lib/api";
import { errorText } from "@/lib/errors";

/**
 * CZL Connect 的登录、云同步、空间与订阅状态, 全局一份。状态都在 Go 里, 这里只缓存最近一次结果
 * 并订阅推送; 登录是可选的, 未登录时只有本机空间。
 */
interface CloudContext {
  account: AccountStatus | null;
  user: AccountUser | null;
  cloud: CloudState | null;
  spaces: SpacesView | null;
  billing: BillingState | null;
  /** 当前空间 (未加载时为本机)。 */
  current: SpaceView;
  refreshSpaces: () => Promise<void>;
  /** 联网刷新同步状态与空间列表。 */
  refreshCloud: () => Promise<void>;
  refreshBilling: (force?: boolean) => Promise<void>;
  switchSpace: (key: string) => Promise<void>;
  /** 切到某个团队的空间 (团队空间标识带账号, 按组织 id 在最新列表里找)。 */
  openOrgSpace: (orgId: string) => Promise<boolean>;
}

const localSpace: SpaceView = {
  key: LOCAL_SPACE, kind: "local", name: "", orgId: "", role: "", readOnly: false, need: "", removed: false,
  pending: 0, status: "", error: "", syncedAt: "", connections: 0, scripts: 0,
};

const Ctx = createContext<CloudContext | null>(null);

export function useCloud(): CloudContext {
  const c = useContext(Ctx);
  if (!c) throw new Error("useCloud outside CloudProvider");
  return c;
}

/** 空间在界面上的名字。 */
export function spaceLabel(s: Pick<SpaceView, "kind" | "name">): string {
  if (s.kind === "local") return t("本机");
  if (s.kind === "personal") return t("我的云同步");
  return s.name || t("团队");
}

export function CloudProvider({ children }: { children: React.ReactNode }) {
  const [account, setAccount] = useState<AccountStatus | null>(null);
  const [cloud, setCloud] = useState<CloudState | null>(null);
  const [spaces, setSpaces] = useState<SpacesView | null>(null);
  const [billing, setBilling] = useState<BillingState | null>(null);
  const [joinCode, setJoinCode] = useState("");
  // 事件回调里要读最新的登录状态, 不随 effect 重新订阅
  const loggedInRef = useRef(false);
  // 未登录时收到的邀请码, 登录后再弹
  const pendingCodeRef = useRef("");

  const refreshSpaces = useCallback(async () => {
    try {
      setSpaces(await api.ListSpaces());
    } catch {
      /* 启动失败时由主界面显示错误页 */
    }
  }, []);

  const refreshCloud = useCallback(async () => {
    setCloud(await api.CloudRefresh());
    await refreshSpaces();
  }, [refreshSpaces]);

  const refreshBilling = useCallback(async (force = false) => {
    setBilling(await api.BillingRefresh(force));
  }, []);

  const switchSpace = useCallback(
    async (key: string) => {
      try {
        await api.SwitchSpace(key);
        await refreshSpaces();
      } catch (e) {
        toast.error(errorText(e));
      }
    },
    [refreshSpaces],
  );

  const openOrgSpace = useCallback(
    async (orgId: string) => {
      const list = await api.ListSpaces().catch(() => null);
      const sp = list?.spaces.find((s) => s.orgId === orgId);
      if (!sp) return false;
      await switchSpace(sp.key);
      return true;
    },
    [switchSpace],
  );

  const takeJoin = useCallback(() => {
    api.TakePendingJoin().then((code) => code && setJoinCode(code)).catch(() => {});
  }, []);

  useEffect(() => {
    api
      .AccountStatus()
      .then((s) => {
        setAccount(s);
        loggedInRef.current = s.loggedIn;
        if (s.loggedIn) takeJoin();
      })
      .catch(() => setAccount(null));
    queueMicrotask(() => void refreshSpaces());
    api.CloudState().then(setCloud).catch(() => {});
    api.BillingState().then(setBilling).catch(() => {});

    const offs = [
      onEvent<AccountStatus>(Events.authChanged, (s) => {
        setAccount(s);
        loggedInRef.current = s.loggedIn;
        void refreshSpaces();
        if (s.loggedIn) {
          toast.success(t("已登录 CZL Connect"));
          if (pendingCodeRef.current) {
            setJoinCode(pendingCodeRef.current);
            pendingCodeRef.current = "";
          } else takeJoin();
        }
      }),
      onEvent<string>(Events.authFailed, (e) => toast.error(t("登录失败: {0}", errorText(e)))),
      onEvent<CloudState>(Events.cloudState, (s) => {
        setCloud(s);
        void refreshSpaces();
      }),
      onEvent<BillingState>(Events.billingChanged, (b) => {
        setBilling(b);
        void refreshSpaces();
      }),
      onEvent(Events.spaceChanged, () => void refreshSpaces()),
      onEvent<ConflictInfo>(Events.cloudConflict, () =>
        toast.warning(t("同一条记录在两处都改过: 已采用云端版本, 本机的修改另存为「冲突副本」")),
      ),
      onEvent<string>(Events.teamJoin, (code) => {
        // 事件已经带来邀请码, 取走 Go 侧记着的那份, 免得之后重新登录时再弹一次
        void api.TakePendingJoin().catch(() => {});
        if (loggedInRef.current) setJoinCode(code);
        else {
          pendingCodeRef.current = code;
          toast.info(t("收到团队邀请, 登录后即可加入"));
        }
      }),
    ];
    // 回到窗口时刷新订阅 (Go 侧 30 秒内用缓存), 在浏览器里付完款回来能立刻看到。
    const onFocus = () => void api.BillingRefresh(false).then(setBilling).catch(() => {});
    window.addEventListener("focus", onFocus);
    return () => {
      offs.forEach((off) => off());
      window.removeEventListener("focus", onFocus);
    };
  }, [refreshSpaces, takeJoin]);

  const value = useMemo<CloudContext>(() => {
    const current = spaces?.spaces.find((s) => s.key === spaces.current) ?? localSpace;
    return {
      account,
      user: (account?.loggedIn ? (account.user as AccountUser) : null) ?? null,
      cloud,
      spaces,
      billing,
      current,
      refreshSpaces,
      refreshCloud,
      refreshBilling,
      switchSpace,
      openOrgSpace,
    };
  }, [account, cloud, spaces, billing, refreshSpaces, refreshCloud, refreshBilling, switchSpace, openOrgSpace]);

  return (
    <Ctx.Provider value={value}>
      {children}
      <TeamJoinDialog
        code={joinCode}
        loggedIn={!!account?.loggedIn}
        onClose={() => setJoinCode("")}
        onJoined={async (orgId) => {
          setJoinCode("");
          await refreshCloud();
          await openOrgSpace(orgId);
        }}
      />
    </Ctx.Provider>
  );
}
