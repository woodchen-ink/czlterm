"use client";

import { ChevronDownIcon, CopyIcon, FileCode2Icon, MoreHorizontalIcon, PencilIcon, PlayIcon, Trash2Icon } from "lucide-react";

import { FileBrowser } from "@/components/file-browser";
import type { ConnectionActions } from "@/components/connection-list";
import { OSIcon } from "@/components/os-icon";
import { ProtocolIcon, protocolLabel } from "@/components/protocol-icon";
import { SystemInfo } from "@/components/system-info";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import type { ConnectionView, Script } from "@/lib/api";
import { formatTime } from "@/lib/format";

export type DetailTab = "overview" | "files";

/** 右侧面板: 连接概览与 SFTP 文件管理。 */
export function ConnectionDetail({
  c,
  scripts,
  tab,
  onTabChange,
  actions,
}: {
  c: ConnectionView;
  scripts: Script[];
  tab: DetailTab;
  onTabChange: (t: DetailTab) => void;
  actions: ConnectionActions;
}) {
  const isSSH = c.protocol === "ssh";
  const current = isSSH ? tab : "overview";
  return (
    <Tabs value={current} onValueChange={(v) => onTabChange(v as DetailTab)} className="flex h-full min-h-0 flex-col gap-0">
      <div className="flex shrink-0 items-center gap-3 border-b px-4 py-3">
        {c.osId ? (
          <OSIcon osId={c.osId} title={c.osName} className="size-5" />
        ) : (
          <ProtocolIcon protocol={c.protocol} className="text-muted-foreground size-5" />
        )}
        <div className="min-w-0 flex-1">
          <h1 className="truncate text-base font-semibold">{c.name}</h1>
          <p className="text-muted-foreground truncate text-xs">
            {c.username ? `${c.username}@` : ""}
            {c.host}:{c.port}
          </p>
        </div>
        {isSSH && (
          <TabsList>
            <TabsTrigger value="overview">概览</TabsTrigger>
            <TabsTrigger value="files">文件</TabsTrigger>
          </TabsList>
        )}
        {isSSH && (
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="outline">
                <FileCode2Icon /> 脚本 <ChevronDownIcon />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="czl-scroll max-h-80 min-w-48 overflow-y-auto">
              {scripts.length === 0 && <DropdownMenuLabel className="text-muted-foreground font-normal">还没有脚本</DropdownMenuLabel>}
              {scripts.map((s) => (
                <DropdownMenuItem key={s.id} onSelect={() => actions.runScript(c, s)}>
                  <PlayIcon /> {s.group ? `${s.group} / ${s.name}` : s.name}
                </DropdownMenuItem>
              ))}
              <DropdownMenuSeparator />
              <DropdownMenuItem onSelect={actions.manageScripts}>管理脚本…</DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        )}
        <Button onClick={() => actions.connect(c)}>
          <PlayIcon /> 连接
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" aria-label="更多">
              <MoreHorizontalIcon />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onSelect={() => actions.edit(c)}>
              <PencilIcon /> 编辑
            </DropdownMenuItem>
            <DropdownMenuItem onSelect={() => actions.duplicate(c)}>
              <CopyIcon /> 复制
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem variant="destructive" onSelect={() => actions.remove(c)}>
              <Trash2Icon /> 删除
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      <TabsContent value="overview" className="czl-scroll min-h-0 flex-1 space-y-6 overflow-auto p-4">
        {isSSH && <SystemInfo connId={c.id} />}
        <h2 className="text-sm font-semibold">连接配置</h2>
        <dl className="grid max-w-2xl grid-cols-[8rem_1fr] gap-x-4 gap-y-2 text-sm">
          <Row label="协议">
            <Badge variant="secondary">{protocolLabel(c.protocol)}</Badge>
          </Row>
          <Row label="主机">{c.host}</Row>
          <Row label="端口">{c.port}</Row>
          <Row label="用户名">{c.username || <Muted>{c.auth === "vault" ? "取自保险库条目" : "系统默认"}</Muted>}</Row>
          <Row label="分组">{c.group || <Muted>未分组</Muted>}</Row>
          <Row label="凭据">
            {c.auth === "vault" ? (
              <span className="space-y-0.5">
                {c.vaultItem && <span className="block">密码: {c.vaultItemName || <Muted>保险库锁定中</Muted>}</span>}
                {c.keyItem && <span className="block">私钥: {c.keyItemName || <Muted>保险库锁定中</Muted>}</span>}
                {!c.vaultItem && !c.keyItem && <Muted>Vaultwarden (未选条目)</Muted>}
              </span>
            ) : (
              <Muted>{isSSH ? "系统默认 (~/.ssh、系统 agent)" : "连接时手动输入"}</Muted>
            )}
          </Row>
          {isSSH && <Row label="跳板机">{c.jump ? c.jumpName || <Muted>已删除</Muted> : <Muted>无</Muted>}</Row>}
          {c.notes && <Row label="备注">{<span className="whitespace-pre-wrap">{c.notes}</span>}</Row>}
          <Row label="更新于">{formatTime(c.updatedAt)}</Row>
        </dl>
      </TabsContent>
      {isSSH && (
        <TabsContent value="files" className="min-h-0 flex-1">
          {current === "files" && <FileBrowser connId={c.id} />}
        </TabsContent>
      )}
    </Tabs>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="min-w-0 break-all">{children}</dd>
    </>
  );
}

function Muted({ children }: { children: React.ReactNode }) {
  return <span className="text-muted-foreground">{children}</span>;
}
