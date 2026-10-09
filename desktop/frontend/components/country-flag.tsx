"use client";

import { useEffect, useState } from "react";

import { cn } from "@/lib/utils";

type FlagMap = Record<string, string | undefined>;

// 国旗 SVG 集合有几百 KB, 按需加载成独立分块, 不拖慢首屏; 所有实例共用一次加载。
let flagsPromise: Promise<FlagMap> | null = null;
function loadFlags() {
  flagsPromise ??= import("country-flag-icons/string/3x2").then((m) => m as unknown as FlagMap);
  return flagsPromise;
}

/** 国旗图标。code 为 ISO 3166-1 alpha-2 (大写); 不认识的国家码不显示。用 SVG 而不是 emoji: Windows 不渲染国旗 emoji。 */
export function CountryFlag({ code, title, className }: { code: string; title?: string; className?: string }) {
  const [svg, setSvg] = useState<string>();

  useEffect(() => {
    let alive = true;
    loadFlags()
      .then((m) => alive && setSvg(m[code]))
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [code]);

  if (!svg) return null;
  return (
    // eslint-disable-next-line @next/next/no-img-element -- 内联 data URI, 无需 next/image 优化
    <img
      src={`data:image/svg+xml;charset=utf-8,${encodeURIComponent(svg)}`}
      alt={title ?? code}
      title={title ?? code}
      className={cn("h-3 w-[18px] shrink-0 rounded-[2px] shadow-[0_0_0_1px_rgb(0_0_0/0.08)]", className)}
      draggable={false}
    />
  );
}
