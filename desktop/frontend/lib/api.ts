/**
 * Wails 绑定的薄封装: 生成的绑定收口到这里, 上游签名变化时只改这一处。
 */

import * as App from "@/wailsjs/go/main/App";
import * as VNCWindow from "@/wailsjs/go/main/VNCWindow";
import { BrowserOpenURL, EventsOn } from "@/wailsjs/runtime/runtime";
import type { billing, cloudsync, conn, cred, facts, launch, main, orgs, remotefs, script, settings, space, term, vault, vnc } from "@/wailsjs/go/models";

export type Connection = conn.Connection;
export type ConnectionView = main.ConnectionView;
export type ConnectionList = main.ConnectionList;
export type LaunchResult = launch.Result;
export type Listing = remotefs.Listing;
export type Entry = remotefs.Entry;
export type Settings = settings.Settings;
export type SettingsView = main.SettingsView;
export type MCPConfig = main.MCPConfig;
export type EditStatus = main.EditStatus;
export type VaultStatus = vault.Status;
export type VaultItem = vault.ItemSummary;
export type UpdateInfo = main.UpdateInfo;
export type AboutInfo = main.AboutInfo;
export type Facts = facts.Facts;
export type Script = script.Script;
export type ScriptList = main.ScriptList;
export type TerminalInfo = term.Info;
export type VNCSession = vnc.Info;
export type VNCWindowParams = main.VNCWindowParams;
export type AccountStatus = main.AccountStatus;
export type SpaceView = main.SpaceView;
export type SpacesView = main.SpacesView;
export type CloudState = cloudsync.State;
export type CloudSpace = cloudsync.SpaceInfo;
export type DeviceRequest = cloudsync.DeviceRequest;
export type MoveResult = space.MoveResult;
export type BillingState = main.BillingState;
export type Plan = billing.Plan;
export type PlanQuote = billing.PlanQuote;
export type AddOn = billing.AddOn;
export type Entitlement = billing.Entitlement;
export type Org = orgs.Org;
export type OrgDetail = orgs.Detail;
export type OrgMember = orgs.Member;
export type NewInviteCode = orgs.NewInviteCode;
export type CredentialSummary = cred.Summary;
export type CredentialList = main.CredentialList;
export type ConnectionReview = main.ConnectionReview;

/** 登录账号信息, 对应 account.User (Status.user 在绑定里是 any)。 */
export interface AccountUser {
  id: string;
  username: string;
  nickname: string;
  email: string;
  avatar: string;
}

/** 新设备等待批准的进度, 对应 cloudsync.DeviceProgress (只经事件推送)。 */
export interface DeviceProgress {
  stage: "code" | "approved" | "failed";
  code?: string;
  error?: string;
}

/** 推送冲突: 服务端版本已落地到 id, 本地版本另存为 copyId, 对应 cloudsync.ConflictInfo。 */
export interface ConflictInfo {
  space: string;
  collection: string;
  id: string;
  copyId: string;
}

export const LOCAL_SPACE = "local";

/** 主机密钥与 known_hosts 不符的提示, 对应 sshx.HostKeyChange (只经事件推送, 绑定里没有生成)。 */
export interface HostKeyChange {
  host: string;
  keyType: string;
  fingerprint: string;
  known: string[] | null;
}

export type Protocol = "ssh" | "rdp" | "vnc";

export const api = App;
/** 独立 VNC 窗口进程里的绑定, 主窗口里只有空参数。 */
export const vncWindow = VNCWindow;

/** 后端推送的事件名, 与 app.go 中的常量一致。 */
export const Events = {
  vaultLocked: "vault:locked",
  vaultUnlocked: "vault:unlocked",
  authChanged: "auth:changed",
  authFailed: "auth:failed",
  teamJoin: "team:join",
  spaceChanged: "space:changed",
  cloudState: "cloud:state",
  cloudChanged: "cloud:changed",
  cloudConflict: "cloud:conflict",
  cloudDevice: "cloud:device",
  billingChanged: "billing:changed",
  billingDone: "billing:done",
  editStatus: "edit:status",
  updateAvailable: "update:available",
  updateProgress: "update:progress",
  factsUpdated: "facts:updated",
  geoUpdated: "geo:updated",
  hostKeyChanged: "hostkey:changed",
  terminalExit: "terminal:exit",
} as const;

/**
 * 当前页面是否是弹出的独立 VNC 窗口。独立窗口进程只绑定 VNCWindow, 没有 App;
 * 浏览器预览两者都没有, 按主窗口处理。
 */
export function isVNCWindow(): boolean {
  if (typeof window === "undefined") return false;
  const main = (window as { go?: { main?: Record<string, unknown> } }).go?.main;
  return !!main?.VNCWindow && !main.App;
}

/** 本机 WebSocket 根地址 (ws://127.0.0.1:端口) 与令牌, 终端在 /terminal, VNC 在 /vnc。 */
export interface LocalEndpoint {
  url: string;
  token: string;
}

let endpoint: Promise<LocalEndpoint> | null = null;
/** 主窗口的本机 WebSocket 端点在本次运行内不变, 取一次缓存起来。 */
export function localEndpoint(): Promise<LocalEndpoint> {
  endpoint ??= App.GetTerminalEndpoint().catch((e) => {
    endpoint = null;
    throw e;
  });
  return endpoint;
}

/** 用系统浏览器打开外部链接。 */
export function openExternal(url: string) {
  BrowserOpenURL(url);
}

export function onEvent<T = unknown>(name: string, fn: (data: T) => void): () => void {
  return EventsOn(name, fn as (...data: unknown[]) => void);
}

/** 新建连接的初始值。 */
export function emptyConnection(protocol: Protocol = "ssh"): Connection {
  return {
    id: "",
    name: "",
    group: "",
    protocol,
    host: "",
    port: 0,
    username: "",
    auth: "system",
    vaultItem: "",
    keyItem: "",
    credential: "",
    jump: "",
    notes: "",
    updatedAt: "",
  };
}

export const defaultPort: Record<Protocol, number> = { ssh: 22, rdp: 3389, vnc: 5900 };

/** 新建脚本的初始值。 */
export function emptyScript(): Script {
  return { id: "", name: "", group: "", content: "", notes: "", updatedAt: "" };
}
