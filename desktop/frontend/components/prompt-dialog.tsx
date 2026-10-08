"use client";

import { useState } from "react";

import { Button } from "@/components/ui/button";
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";

export interface PromptRequest {
  title: string;
  initial: string;
  confirm: string;
  onSubmit: (value: string) => Promise<void> | void;
}

/** 单行输入框对话框, 用于新建文件夹、重命名。 */
export function PromptDialog({ request, onClose }: { request: PromptRequest | null; onClose: () => void }) {
  return (
    <Dialog open={!!request} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-sm">
        {/* 按请求重新挂载, 输入框的初始值只在挂载时读取。 */}
        {request && <PromptForm key={`${request.title}:${request.initial}`} request={request} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  );
}

function PromptForm({ request, onClose }: { request: PromptRequest; onClose: () => void }) {
  const [value, setValue] = useState(request.initial);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!value.trim()) return;
    setBusy(true);
    try {
      await request.onSubmit(value.trim());
      onClose();
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <DialogHeader>
        <DialogTitle>{request.title}</DialogTitle>
      </DialogHeader>
      <form id="prompt-form" onSubmit={submit}>
        <Input autoFocus value={value} onChange={(e) => setValue(e.target.value)} onFocus={(e) => e.target.select()} />
      </form>
      <DialogFooter>
        <Button variant="outline" onClick={onClose}>
          取消
        </Button>
        <Button type="submit" form="prompt-form" disabled={busy || !value.trim()}>
          {request.confirm}
        </Button>
      </DialogFooter>
    </>
  );
}
