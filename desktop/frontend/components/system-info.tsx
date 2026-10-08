"use client";

import { useCallback, useEffect, useState } from "react";
import { Loader2Icon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { OSIcon } from "@/components/os-icon";
import { UnlockCancelled, useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { api, Events, onEvent, type Facts } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes, formatDuration, formatTime } from "@/lib/format";

/** 概览页里的机器配置与系统信息。SSH 连接一次后自动采集, 也可手动刷新。 */
export function SystemInfo({ connId }: { connId: string }) {
  const { withVault } = useVault();
  const [facts, setFacts] = useState<Facts | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    api
      .GetFacts(connId)
      .then(setFacts)
      .catch(() => {});
  }, [connId]);

  useEffect(() => {
    load();
    return onEvent<string>(Events.factsUpdated, (id) => id === connId && load());
  }, [connId, load]);

  async function refresh() {
    setBusy(true);
    try {
      setFacts(await withVault(() => api.RefreshFacts(connId)));
    } catch (e) {
      if (!(e instanceof UnlockCancelled)) toast.error(errorText(e));
    } finally {
      setBusy(false);
    }
  }

  const header = (
    <div className="flex items-center justify-between">
      <h2 className="text-sm font-semibold">系统信息</h2>
      <Button variant="ghost" size="sm" onClick={refresh} disabled={busy}>
        {busy ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} {facts?.collectedAt ? "刷新" : "立即采集"}
      </Button>
    </div>
  );

  if (!facts?.collectedAt) {
    return (
      <section className="space-y-2">
        {header}
        <p className="text-muted-foreground text-sm">连接一次后自动采集系统、CPU、内存与磁盘信息。</p>
      </section>
    );
  }

  const memUsed = facts.memoryBytes && facts.memoryAvailableBytes ? facts.memoryBytes - facts.memoryAvailableBytes : 0;
  return (
    <section className="space-y-3">
      {header}
      <div className="flex items-center gap-3 rounded-lg border p-3">
        <OSIcon osId={facts.osId || "linux"} className="size-8" />
        <div className="min-w-0">
          <p className="truncate text-sm font-medium">{facts.osName || facts.kernel}</p>
          <p className="text-muted-foreground truncate text-xs">
            {[facts.hostname, facts.kernel, facts.arch].filter(Boolean).join(" · ")}
          </p>
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <Stat label="CPU" value={facts.cores ? `${facts.cores} 核` : "—"} sub={facts.cpu} />
        <Meter label="内存" used={memUsed} total={facts.memoryBytes} />
        <Meter label="磁盘 /" used={facts.diskUsedBytes} total={facts.diskBytes} />
      </div>
      <p className="text-muted-foreground text-xs">
        {facts.uptimeSeconds ? `已运行 ${formatDuration(facts.uptimeSeconds)} · ` : ""}
        {facts.load ? `负载 ${facts.load} · ` : ""}采集于 {formatTime(facts.collectedAt)}
      </p>
    </section>
  );
}

function Stat({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <div className="min-w-0 rounded-lg border p-3">
      <p className="text-muted-foreground text-xs">{label}</p>
      <p className="text-base font-semibold tabular-nums">{value}</p>
      {sub && <p className="text-muted-foreground truncate text-xs" title={sub}>{sub}</p>}
    </div>
  );
}

function Meter({ label, used, total }: { label: string; used: number; total: number }) {
  const pct = total && used ? Math.min(100, Math.round((used / total) * 100)) : 0;
  return (
    <div className="min-w-0 rounded-lg border p-3">
      <p className="text-muted-foreground text-xs">{label}</p>
      <p className="text-base font-semibold tabular-nums">{total ? formatBytes(total) : "—"}</p>
      {total > 0 && used > 0 && (
        <>
          <div className="bg-muted mt-1.5 h-1.5 overflow-hidden rounded-full">
            <div className={pct >= 90 ? "bg-destructive h-full" : "bg-accent h-full"} style={{ width: `${pct}%` }} />
          </div>
          <p className="text-muted-foreground mt-1 text-xs tabular-nums">
            已用 {formatBytes(used)} · {pct}%
          </p>
        </>
      )}
    </div>
  );
}
