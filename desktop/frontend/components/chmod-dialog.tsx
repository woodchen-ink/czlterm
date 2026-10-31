"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Select, SelectContent, SelectGroup, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select";
import type { Entry } from "@/lib/api";

/** 与后端 remotefs.ChmodScope 一致。 */
export type ChmodScope = "self" | "all" | "dirs" | "files";

export interface ChmodRequest {
  entries: Entry[];
  onSubmit: (mode: number, scope: ChmodScope) => Promise<void> | void;
}

/** 修改权限对话框: 勾选框与八进制输入双向同步, 目录可选择递归范围。 */
export function ChmodDialog({ request, onClose }: { request: ChmodRequest | null; onClose: () => void }) {
  return (
    <Dialog open={!!request} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        {request && <ChmodForm key={request.entries.map((e) => e.path).join("\n")} request={request} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

const who = [
  { label: "所有者", shift: 6 },
  { label: "用户组", shift: 3 },
  { label: "其他人", shift: 0 },
];
const bits = [
  { label: "读", bit: 4 },
  { label: "写", bit: 2 },
  { label: "执行", bit: 1 },
];
const special = [
  { label: "SUID", bit: 0o4000 },
  { label: "SGID", bit: 0o2000 },
  { label: "Sticky", bit: 0o1000 },
];

function toOctal(mode: number): string {
  return mode.toString(8).padStart(mode > 0o777 ? 4 : 3, "0");
}

function ChmodForm({ request, onClose }: { request: ChmodRequest; onClose: () => void }) {
  useLanguage();
  const { entries } = request;
  const [mode, setMode] = useState(entries[0]?.perm ?? 0o644);
  const [octal, setOctal] = useState(toOctal(entries[0]?.perm ?? 0o644));
  const [scope, setScope] = useState<ChmodScope>("self");
  const [busy, setBusy] = useState(false);
  const hasDir = entries.some((e) => e.isDir);
  const octalValid = /^[0-7]{1,4}$/.test(octal);

  function update(next: number) {
    setMode(next);
    setOctal(toOctal(next));
  }

  function toggle(bit: number, on: boolean) {
    update(on ? mode | bit : mode & ~bit);
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!octalValid) return;
    setBusy(true);
    try {
      await request.onSubmit(mode, hasDir ? scope : "self");
      onClose();
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("修改权限")}</DialogTitle>
        <DialogDescription className="truncate font-mono text-xs">
          {entries.length === 1 ? entries[0].path : t("已选 {0} 项", entries.length)}
        </DialogDescription>
      </DialogHeader>
      <form id="chmod-form" className="flex flex-col gap-4" onSubmit={submit}>
        <table className="text-sm">
          <thead className="text-muted-foreground text-xs">
            <tr>
              <th />
              {bits.map((b) => (
                <th key={b.bit} className="w-16 py-1 font-medium">{t(b.label)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {who.map((w) => (
              <tr key={w.shift}>
                <td className="py-1.5 pr-3">{t(w.label)}</td>
                {bits.map((b) => {
                  const bit = b.bit << w.shift;
                  return (
                    <td key={b.bit} className="text-center">
                      <Checkbox
                        className="mx-auto"
                        aria-label={`${t(w.label)} ${t(b.label)}`}
                        checked={(mode & bit) !== 0}
                        onCheckedChange={(v) => toggle(bit, v === true)}
                      />
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
        <div className="flex items-center gap-4 text-sm">
          {special.map((s) => (
            <label key={s.bit} className="flex items-center gap-2">
              <Checkbox checked={(mode & s.bit) !== 0} onCheckedChange={(v) => toggle(s.bit, v === true)} />
              <span className="font-mono text-xs">{s.label}</span>
            </label>
          ))}
          <Input
            className="ml-auto h-8 w-20 text-center font-mono"
            aria-label={t("八进制")}
            aria-invalid={!octalValid}
            value={octal}
            maxLength={4}
            onChange={(e) => {
              const v = e.target.value.trim();
              setOctal(v);
              if (/^[0-7]{1,4}$/.test(v)) setMode(parseInt(v, 8));
            }}
          />
        </div>
        {hasDir && (
          <Select value={scope} onValueChange={(v) => setScope(v as ChmodScope)}>
            <SelectTrigger className="w-full" aria-label={t("应用范围")}><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectGroup>
                <SelectItem value="self">{t("仅所选项")}</SelectItem>
                <SelectItem value="all">{t("递归应用到所有子目录和文件")}</SelectItem>
                <SelectItem value="dirs">{t("递归应用到子目录 (含自身)")}</SelectItem>
                <SelectItem value="files">{t("递归应用到子文件 (不含目录)")}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        )}
      </form>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          {t("取消")}
        </Button>
        <Button type="submit" form="chmod-form" disabled={busy || !octalValid}>
          {t("应用")}
        </Button>
      </DialogFooter>
    </>
  );
}
