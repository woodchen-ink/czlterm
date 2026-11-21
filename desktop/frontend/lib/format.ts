/** 纯展示层格式化。 */

import { t } from "@/lib/i18n";

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v < 10 ? v.toFixed(1) : Math.round(v)} ${units[i]}`;
}

/** RFC3339 时间按本地时区显示为 YYYY-MM-DD HH:mm。 */
export function formatTime(iso: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const p = (x: number) => String(x).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** 远程 POSIX 路径的上级目录。 */
export function parentPath(p: string): string {
  if (!p || p === "/") return "/";
  const trimmed = p.replace(/\/+$/, "");
  const i = trimmed.lastIndexOf("/");
  if (i <= 0) return "/";
  return trimmed.slice(0, i);
}

/** 拼接远程目录与文件名。name 已是绝对路径时原样返回。 */
export function joinPath(dir: string, name: string): string {
  if (name.startsWith("/")) return name;
  return dir.endsWith("/") ? dir + name : `${dir}/${name}`;
}

/** 秒数显示为 "3 天 4 小时" / "5 小时 12 分" / "8 分"。 */
export function formatDuration(sec: number): string {
  const d = Math.floor(sec / 86400);
  const h = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60);
  if (d > 0) return t("{0} 天 {1} 小时", d, h);
  if (h > 0) return t("{0} 小时 {1} 分", h, m);
  return t("{0} 分", m);
}

/** 金额 (分) 按币种显示: 人民币 ¥34, 美元 $9.99, 其它带币种代码。 */
export function formatMoney(cents: number, currency: string): string {
  const v = cents / 100;
  const s = Number.isInteger(v) ? String(v) : v.toFixed(2);
  const c = (currency || "CNY").toUpperCase();
  if (c === "CNY") return `¥${s}`;
  if (c === "USD") return `$${s}`;
  return `${s} ${c}`;
}

/** 距今多久 (刚刚 / N 分钟前 / N 小时前 / 日期)。 */
export function formatAgo(iso: string): string {
  if (!iso) return "";
  const sec = Math.round((Date.now() - new Date(iso).getTime()) / 1000);
  if (Number.isNaN(sec)) return iso;
  if (sec < 60) return t("刚刚");
  if (sec < 3600) return t("{0} 分钟前", Math.floor(sec / 60));
  if (sec < 86400) return t("{0} 小时前", Math.floor(sec / 3600));
  return formatTime(iso);
}

/** Go time.Time 经绑定是 RFC3339 字符串, 零值是 0001 年; 转成日期或空串。 */
export function formatDate(v: unknown): string {
  if (typeof v !== "string" || !v || v.startsWith("0001-")) return "";
  return formatTime(v).slice(0, 10);
}
