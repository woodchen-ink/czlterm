"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useCallback, useEffect, useState } from "react";
import { CopyIcon, Loader2Icon, RefreshCwIcon } from "lucide-react";
import { toast } from "sonner";

import { OSIcon } from "@/components/os-icon";
import { UnlockCancelled, useVault } from "@/components/vault-provider";
import { Button } from "@/components/ui/button";
import { api, Events, onEvent, type Facts } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatBytes, formatDuration, formatTime } from "@/lib/format";

/** 概览页里的机器配置与系统信息。SSH 连接一次后自动采集, 也可手动刷新。 */
export function SystemInfo({ connId }: { connId: string }) {
  useLanguage();
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
      <h2 className="text-sm font-semibold">{t("系统信息")}</h2>
      <Button variant="ghost" size="sm" onClick={refresh} disabled={busy}>
        {busy ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />} {facts?.collectedAt ? t("刷新") : t("立即采集")}
      </Button>
    </div>
  );

  if (!facts?.collectedAt) {
    return (
      <section className="space-y-2">
        {header}
        <p className="text-muted-foreground text-sm">{t("连接一次后自动采集系统、CPU、内存与磁盘信息。")}
        </p>
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
          <AddressList label="IPv4" addrs={facts.ipv4} />
          <AddressList label="IPv6" addrs={facts.ipv6} />
        </div>
      </div>
      <div className="grid gap-3 sm:grid-cols-3">
        <Stat label="CPU" value={facts.cores ? t("{0} 核", facts.cores) : "—"} sub={facts.cpu} />
        <Meter label={t("内存")} used={memUsed} total={facts.memoryBytes} />
        <Meter label={t("磁盘 /")} used={facts.diskUsedBytes} total={facts.diskBytes} />
      </div>
      <p className="text-muted-foreground text-xs">
        {facts.uptimeSeconds ? t("已运行 {0} · ", formatDuration(facts.uptimeSeconds)) : ""}
        {facts.load ? t("负载 {0} · ", facts.load) : ""}{t("采集于 {0}", formatTime(facts.collectedAt))}
      </p>
    </section>
  );
}

/** 一组网卡地址, 点击单个地址复制。旧缓存里没有这个字段, 为空时不渲染。 */
function AddressList({ label, addrs }: { label: string; addrs?: string[] | null }) {
  useLanguage();
  if (!addrs?.length) return null;
  function copy(addr: string) {
    navigator.clipboard.writeText(addr).then(
      () => toast.success(t("已复制 {0}", addr)),
      () => toast.error(t("复制失败")),
    );
  }
  return (
    <div className="mt-1 flex flex-wrap items-center gap-1">
      <span className="text-muted-foreground w-8 shrink-0 text-xs">{label}</span>
      {addrs.map((addr) => (
        <button
          key={addr}
          type="button"
          onClick={() => copy(addr)}
          title={t("复制 {0}", addr)}
          className="hover:bg-muted group inline-flex max-w-full items-center gap-1 rounded px-1 font-mono text-xs"
        >
          <span className="truncate">{addr}</span>
          <CopyIcon className="text-muted-foreground size-3 shrink-0 opacity-0 group-hover:opacity-100 group-focus-visible:opacity-100" />
        </button>
      ))}
    </div>
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
  useLanguage();
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
            {t("已用 {0} · {1}%", formatBytes(used), pct)}
          </p>
        </>
      )}
    </div>
  );
}
