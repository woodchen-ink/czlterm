/**
 * Wails 绑定的薄封装: 生成的绑定收口到这里, 上游签名变化时只改这一处。
 */

import * as App from "@/wailsjs/go/main/App";
import { BrowserOpenURL, EventsOn } from "@/wailsjs/runtime/runtime";
import type { conn, facts, launch, main, remotefs, settings, vault } from "@/wailsjs/go/models";

export type Connection = conn.Connection;
export type ConnectionView = main.ConnectionView;
export type ConnectionList = main.ConnectionList;
export type LaunchResult = launch.Result;
export type Listing = remotefs.Listing;
export type Entry = remotefs.Entry;
export type Settings = settings.Settings;
export type SettingsView = main.SettingsView;
export type SyncStatus = main.SyncStatus;
export type MCPConfig = main.MCPConfig;
export type EditStatus = main.EditStatus;
export type VaultStatus = vault.Status;
export type VaultItem = vault.ItemSummary;
export type UpdateInfo = main.UpdateInfo;
export type AboutInfo = main.AboutInfo;
export type Facts = facts.Facts;

export type Protocol = "ssh" | "rdp" | "vnc";

export const api = App;

/** 后端推送的事件名, 与 app.go 中的常量一致。 */
export const Events = {
  vaultLocked: "vault:locked",
  syncStatus: "sync:status",
  editStatus: "edit:status",
  updateAvailable: "update:available",
  updateProgress: "update:progress",
  factsUpdated: "facts:updated",
} as const;

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
    jump: "",
    notes: "",
    updatedAt: "",
  };
}

export const defaultPort: Record<Protocol, number> = { ssh: 22, rdp: 3389, vnc: 5900 };
