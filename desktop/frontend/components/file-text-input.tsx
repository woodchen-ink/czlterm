"use client";

import { t } from "@/lib/i18n";
import { useLanguage } from "@/components/language-provider";

import { useRef, useState } from "react";
import { ClipboardPasteIcon, FileUpIcon, XIcon } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { Textarea } from "@/components/ui/textarea";
import { cn } from "@/lib/utils";

/**
 * 可粘贴、拖入文件、选择文件的多行文本框 (恢复码、私钥这类常放在文件里的内容)。
 * 文件只在本页读取, 不经后端、不上传。extract 从文件内容里取出需要的那段, 取不到返回 null。
 */
export function FileTextInput({
  value,
  onChange,
  extract,
  placeholder,
  rows = 3,
  maxBytes = 64 * 1024,
  invalidHint,
  disabled,
  className,
  autoFocus,
}: {
  value: string;
  onChange: (v: string) => void;
  extract?: (text: string) => string | null;
  placeholder?: string;
  rows?: number;
  maxBytes?: number;
  /** 文件里找不到需要的内容时的提示 */
  invalidHint?: string;
  disabled?: boolean;
  className?: string;
  autoFocus?: boolean;
}) {
  useLanguage();
  const fileRef = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);
  const [fileName, setFileName] = useState("");

  function accept(text: string, from: string) {
    const got = extract ? extract(text) : text.trim();
    if (!got) {
      toast.error(invalidHint ?? t("没有在 {0} 里找到可用的内容", from));
      return;
    }
    onChange(got);
  }

  async function readFile(file: File) {
    if (file.size > maxBytes) {
      toast.error(t("文件太大, 不像是要找的内容: {0}", file.name));
      return;
    }
    try {
      const text = await file.text();
      setFileName(file.name);
      accept(text, file.name);
    } catch {
      toast.error(t("读不了文件 {0}", file.name));
    }
  }

  async function paste() {
    try {
      const text = await navigator.clipboard.readText();
      if (!text.trim()) {
        toast.error(t("剪贴板是空的"));
        return;
      }
      setFileName("");
      accept(text, t("剪贴板"));
    } catch {
      toast.error(t("无法读取剪贴板, 请在框里按 Ctrl+V / ⌘V 粘贴"));
    }
  }

  return (
    <div
      className={cn("space-y-1.5", className)}
      onDragOver={(e) => {
        if (disabled) return;
        e.preventDefault();
        setDragging(true);
      }}
      onDragLeave={() => setDragging(false)}
      onDrop={(e) => {
        e.preventDefault();
        setDragging(false);
        const file = e.dataTransfer.files?.[0];
        if (file && !disabled) void readFile(file);
      }}
    >
      <div className="relative">
        <Textarea
          rows={rows}
          value={value}
          disabled={disabled}
          autoFocus={autoFocus}
          spellCheck={false}
          autoComplete="off"
          placeholder={placeholder}
          className={cn("font-mono text-xs transition-colors", dragging && "border-primary bg-primary/5 ring-primary/30 ring-2")}
          onChange={(e) => {
            setFileName("");
            onChange(e.target.value);
          }}
        />
        {dragging && (
          <div className="text-primary pointer-events-none absolute inset-0 flex items-center justify-center gap-2 text-sm font-medium">
            <FileUpIcon className="size-4" /> {t("松开即可读取文件")}
          </div>
        )}
      </div>
      <div className="flex flex-wrap items-center gap-1">
        <Button type="button" variant="outline" size="xs" onClick={paste} disabled={disabled}>
          <ClipboardPasteIcon /> {t("粘贴")}
        </Button>
        <Button type="button" variant="outline" size="xs" onClick={() => fileRef.current?.click()} disabled={disabled}>
          <FileUpIcon /> {t("选择文件")}
        </Button>
        <span className="text-muted-foreground min-w-0 flex-1 truncate text-xs">
          {fileName ? t("已从 {0} 读取", fileName) : t("也可以把文件拖到框里")}
        </span>
        {value && !disabled && (
          <Button
            type="button"
            variant="ghost"
            size="icon-xs"
            aria-label={t("清空")}
            onClick={() => {
              setFileName("");
              onChange("");
            }}
          >
            <XIcon />
          </Button>
        )}
      </div>
      <input
        ref={fileRef}
        type="file"
        className="hidden"
        onChange={(e) => {
          const file = e.target.files?.[0];
          e.target.value = "";
          if (file) void readFile(file);
        }}
      />
    </div>
  );
}
