"use client";

import { useMemo, useState } from "react";
import { ArrowLeftIcon, FileCode2Icon, Loader2Icon, PlayIcon, PlusIcon, SearchIcon, Trash2Icon } from "lucide-react";
import { toast } from "sonner";

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
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import { Textarea } from "@/components/ui/textarea";
import { api, emptyScript, type ConnectionView, type Script } from "@/lib/api";
import { errorText } from "@/lib/errors";
import { formatTime } from "@/lib/format";
import { cn } from "@/lib/utils";

/** 未保存修改时等待确认的去向: 切到另一个脚本 / 新建 / 返回。 */
type Pending = { kind: "select"; script: Script } | { kind: "new" } | { kind: "close" };

/** 整页脚本库: 左侧脚本列表, 右侧编辑器; 可直接选一台 SSH 服务器运行。 */
export function ScriptsPage({
  scripts,
  groups,
  connections,
  initialConnId,
  onClose,
  onChanged,
  onRun,
}: {
  scripts: Script[];
  groups: string[];
  connections: ConnectionView[];
  /** 打开页面时选中的连接, 作为默认运行目标。 */
  initialConnId: string;
  onClose: () => void;
  onChanged: () => Promise<void>;
  onRun: (c: ConnectionView, s: Script) => Promise<void>;
}) {
  const [query, setQuery] = useState("");
  // saved 是编辑器对应的已保存版本, 新建时为空脚本; form 是编辑中的内容。
  const [saved, setSaved] = useState<Script>(() => scripts[0] ?? emptyScript());
  const [form, setForm] = useState<Script>(saved);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [pending, setPending] = useState<Pending | null>(null);
  const [removing, setRemoving] = useState(false);
  const sshConns = useMemo(() => connections.filter((c) => c.protocol === "ssh"), [connections]);
  const [connId, setConnId] = useState(() =>
    sshConns.some((c) => c.id === initialConnId) ? initialConnId : (sshConns[0]?.id ?? ""),
  );

  const dirty = (["name", "group", "content", "notes"] as const).some((k) => form[k] !== saved[k]);

  // 同步可能从其它设备改了或删了正在看的脚本: 没有未保存修改时跟上磁盘版本, 避免运行的与看到的不一致。
  const [prevScripts, setPrevScripts] = useState(scripts);
  if (scripts !== prevScripts) {
    setPrevScripts(scripts);
    if (!dirty && saved.id) {
      const cur = scripts.find((s) => s.id === saved.id);
      const next = cur ?? scripts[0] ?? emptyScript();
      if (!cur || cur.updatedAt !== saved.updatedAt) {
        setSaved(next);
        setForm(next);
      }
    }
  }
  const set = <K extends keyof Script>(k: K, v: Script[K]) => setForm((f) => ({ ...f, [k]: v }));

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return scripts;
    return scripts.filter((s) => [s.name, s.group, s.notes, s.content].some((v) => v.toLowerCase().includes(q)));
  }, [scripts, query]);

  /** 切换编辑对象; 有未保存修改时先确认。 */
  function go(next: Pending, force = false) {
    if (dirty && !force) {
      setPending(next);
      return;
    }
    setError("");
    if (next.kind === "close") onClose();
    else {
      const s = next.kind === "new" ? emptyScript() : next.script;
      setSaved(s);
      setForm(s);
    }
  }

  /** 保存并返回保存后的脚本, 失败时在编辑器下方显示原因。调用方负责 busy。 */
  async function persist(): Promise<Script | null> {
    setError("");
    try {
      const s = await api.SaveScript(form);
      setSaved(s);
      setForm(s);
      await onChanged();
      return s;
    } catch (e) {
      setError(errorText(e));
      return null;
    }
  }

  /** 在 busy 期间执行 fn, 防止连点保存或运行打开多个终端。 */
  async function exclusive(fn: () => Promise<void>) {
    if (busy) return;
    setBusy(true);
    try {
      await fn();
    } finally {
      setBusy(false);
    }
  }

  /** 运行前先保存未保存的修改, 终端里执行的总是磁盘上的版本。 */
  function run() {
    const c = sshConns.find((x) => x.id === connId);
    if (!c) return;
    void exclusive(async () => {
      const s = dirty || !form.id ? await persist() : saved;
      if (s) await onRun(c, s);
    });
  }

  async function remove() {
    setRemoving(false);
    try {
      await api.DeleteScript(saved.id);
      await onChanged();
      const rest = scripts.filter((s) => s.id !== saved.id);
      go(rest[0] ? { kind: "select", script: rest[0] } : { kind: "new" }, true);
    } catch (e) {
      toast.error(errorText(e));
    }
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <Button variant="ghost" size="sm" onClick={() => go({ kind: "close" })}>
          <ArrowLeftIcon /> 返回
        </Button>
        <h1 className="text-sm font-semibold">脚本</h1>
      </div>

      <div className="flex min-h-0 flex-1">
        <nav className="bg-sidebar flex w-64 shrink-0 flex-col border-r">
          <div className="flex shrink-0 items-center gap-1 p-2">
            <div className="relative min-w-0 flex-1">
              <SearchIcon className="text-muted-foreground pointer-events-none absolute top-1/2 left-2 size-3.5 -translate-y-1/2" />
              <Input className="h-7 pl-7 text-sm" placeholder="搜索脚本" value={query} onChange={(e) => setQuery(e.target.value)} />
            </div>
            <Button variant="ghost" size="icon-sm" aria-label="新建脚本" onClick={() => go({ kind: "new" })}>
              <PlusIcon />
            </Button>
          </div>
          <div className="czl-scroll min-h-0 flex-1 space-y-0.5 overflow-y-auto px-1 pb-2">
            {scripts.length === 0 && <p className="text-muted-foreground px-3 py-6 text-center text-sm">还没有脚本</p>}
            {scripts.length > 0 && filtered.length === 0 && (
              <p className="text-muted-foreground px-3 py-6 text-center text-sm">没有匹配的脚本</p>
            )}
            {filtered.map((s, i) => {
              // 列表已按分组排序, 分组变化处插一个组标题。
              const header = s.group && s.group !== filtered[i - 1]?.group ? s.group : null;
              return (
                <div key={s.id}>
                  {header && <p className="text-muted-foreground px-2 pt-2 pb-1 text-xs font-medium">{header}</p>}
                  <button
                    type="button"
                    onClick={() => s.id !== saved.id && go({ kind: "select", script: s })}
                    className={cn(
                      "flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm",
                      s.id === saved.id ? "bg-sidebar-accent text-sidebar-accent-foreground" : "hover:bg-sidebar-accent/60",
                    )}
                  >
                    <FileCode2Icon className="text-muted-foreground size-4 shrink-0" />
                    <span className="truncate">{s.name}</span>
                  </button>
                </div>
              );
            })}
          </div>
        </nav>

        <form
          id="script-form"
          className="flex min-w-0 flex-1 flex-col gap-3 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            void exclusive(async () => {
              if (await persist()) toast.success("脚本已保存");
            });
          }}
        >
          <div className="grid shrink-0 grid-cols-2 gap-2">
            <Field label="名称">
              <Input value={form.name} placeholder="如 重启 nginx" onChange={(e) => set("name", e.target.value)} />
            </Field>
            <Field label="分组">
              <Input list="script-groups" value={form.group} placeholder="可选" onChange={(e) => set("group", e.target.value)} />
              <datalist id="script-groups">
                {groups.map((g) => (
                  <option key={g} value={g} />
                ))}
              </datalist>
            </Field>
          </div>
          <Field label="备注">
            <Input value={form.notes} placeholder="可选" onChange={(e) => set("notes", e.target.value)} />
          </Field>
          <div className="flex min-h-0 flex-1 flex-col gap-1">
            <Label className="text-muted-foreground text-xs">脚本内容</Label>
            <Textarea
              className="czl-scroll min-h-40 flex-1 resize-none font-mono text-xs"
              spellCheck={false}
              value={form.content}
              placeholder={"systemctl restart nginx\nsystemctl status nginx --no-pager"}
              onChange={(e) => set("content", e.target.value)}
            />
            <p className="text-muted-foreground text-xs">
              在终端里连上服务器后执行, 结束后留在 shell 中; 默认用 sh 执行, 首行写 #!/bin/bash 可换解释器
              {saved.updatedAt && ` · 更新于 ${formatTime(saved.updatedAt)}`}
            </p>
          </div>
          {error && <p className="text-destructive shrink-0 text-sm">{error}</p>}
        </form>
      </div>

      <div className="flex shrink-0 items-center gap-2 border-t px-4 py-2">
        {saved.id && (
          <Button variant="ghost" className="text-destructive" onClick={() => setRemoving(true)}>
            <Trash2Icon /> 删除
          </Button>
        )}
        <div className="ml-auto flex items-center gap-2">
          <Select value={connId} onValueChange={setConnId} disabled={sshConns.length === 0}>
            <SelectTrigger className="w-56">
              <SelectValue placeholder="没有 SSH 连接" />
            </SelectTrigger>
            <SelectContent>
              {sshConns.map((c) => (
                <SelectItem key={c.id} value={c.id}>
                  {c.group ? `${c.group} / ${c.name}` : c.name}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant="outline" onClick={run} disabled={busy || !connId}>
            <PlayIcon /> 运行
          </Button>
          <Button type="submit" form="script-form" disabled={busy || !dirty}>
            {busy && <Loader2Icon className="animate-spin" />} 保存
          </Button>
        </div>
      </div>

      <AlertDialog open={!!pending} onOpenChange={(o) => !o && setPending(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>放弃未保存的修改?</AlertDialogTitle>
            <AlertDialogDescription>当前脚本有修改尚未保存。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>继续编辑</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                const p = pending;
                setPending(null);
                if (p) go(p, true);
              }}
            >
              放弃修改
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
      <AlertDialog open={removing} onOpenChange={setRemoving}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>删除脚本 {saved.name}?</AlertDialogTitle>
            <AlertDialogDescription>开启同步时其它设备上的这个脚本也会被删除。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={remove}>删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0 space-y-1">
      <Label className="text-muted-foreground text-xs">{label}</Label>
      {children}
    </div>
  );
}
