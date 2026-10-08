import { MonitorIcon, ScreenShareIcon, SquareTerminalIcon } from "lucide-react";

import { cn } from "@/lib/utils";

const labels: Record<string, string> = { ssh: "SSH", rdp: "RDP", vnc: "VNC" };

/** 协议图标。未知协议回落到终端图标, 不报错。 */
export function ProtocolIcon({ protocol, className }: { protocol: string; className?: string }) {
  const Icon = protocol === "rdp" ? MonitorIcon : protocol === "vnc" ? ScreenShareIcon : SquareTerminalIcon;
  return <Icon className={cn("size-4 shrink-0", className)} aria-label={protocolLabel(protocol)} />;
}

export function protocolLabel(protocol: string): string {
  return labels[protocol] ?? protocol.toUpperCase();
}
