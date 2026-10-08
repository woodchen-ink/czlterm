import {
  siAlmalinux,
  siAlpinelinux,
  siApple,
  siArchlinux,
  siCentos,
  siDebian,
  siDeepin,
  siFedora,
  siFreebsd,
  siGentoo,
  siKalilinux,
  siLinux,
  siLinuxmint,
  siManjaro,
  siNixos,
  siOpensuse,
  siOpenwrt,
  siRaspberrypi,
  siRedhat,
  siRockylinux,
  siUbuntu,
  type SimpleIcon,
} from "simple-icons";

import { cn } from "@/lib/utils";

/**
 * 系统图标。osId 取自 /etc/os-release 的 ID (或 ID_LIKE 的第一项), macOS 为 macos, Windows 为 windows。
 * 按前缀匹配, 未识别的 Linux 回落到 Tux; 完全未知时返回 null, 由调用方显示协议图标。
 * Windows 标志不在 simple-icons 里 (商标原因), 用四格窗格自绘。
 */
const icons: [string, SimpleIcon][] = [
  ["ubuntu", siUbuntu],
  ["debian", siDebian],
  ["centos", siCentos],
  ["rocky", siRockylinux],
  ["almalinux", siAlmalinux],
  ["fedora", siFedora],
  ["arch", siArchlinux],
  ["alpine", siAlpinelinux],
  ["opensuse", siOpensuse],
  ["sles", siOpensuse],
  ["suse", siOpensuse],
  ["rhel", siRedhat],
  ["redhat", siRedhat],
  ["kali", siKalilinux],
  ["linuxmint", siLinuxmint],
  ["manjaro", siManjaro],
  ["gentoo", siGentoo],
  ["nixos", siNixos],
  ["raspbian", siRaspberrypi],
  ["openwrt", siOpenwrt],
  ["deepin", siDeepin],
  ["macos", siApple],
  ["freebsd", siFreebsd],
];

export function OSIcon({ osId, title, className }: { osId: string; title?: string; className?: string }) {
  const id = osId.toLowerCase();
  if (!id) return null;
  if (id === "windows") {
    return (
      <svg viewBox="0 0 24 24" className={cn("size-4 shrink-0", className)} role="img" aria-label={title ?? "Windows"}>
        <title>{title ?? "Windows"}</title>
        <path fill="#0078D4" d="M3 5.5 10.5 4.5v7H3zM11.5 4.3 21 3v8.5h-9.5zM3 12.5h7.5v7L3 18.5zM11.5 12.5H21V21l-9.5-1.3z" />
      </svg>
    );
  }
  const icon = icons.find(([k]) => id.startsWith(k))?.[1] ?? siLinux;
  // 纯黑的品牌色在暗色主题里看不见, 改用前景色。
  const fill = icon.hex === "000000" ? "currentColor" : `#${icon.hex}`;
  return (
    <svg viewBox="0 0 24 24" className={cn("size-4 shrink-0", className)} role="img" aria-label={title ?? icon.title}>
      <title>{title ?? icon.title}</title>
      <path fill={fill} d={icon.path} />
    </svg>
  );
}
