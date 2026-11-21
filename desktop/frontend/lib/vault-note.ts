import { t } from "@/lib/i18n";

/** Vaultwarden 凭据转换结果的一句说明; 没有转换时为空。 */
export function vaultNote(r: { converted: number; credentials: number; skipped: number } | undefined): string | undefined {
  if (!r || (r.converted === 0 && r.skipped === 0)) return undefined;
  const parts: string[] = [];
  if (r.converted > 0) parts.push(t("{0} 条连接的 Vaultwarden 凭据已转为保存的凭据, 随空间加密同步", r.converted));
  if (r.skipped > 0) parts.push(t("{0} 条在保险库里找不到可用条目, 仍引用 Vaultwarden", r.skipped));
  return parts.join("; ");
}
