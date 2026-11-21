// 由 mark.svg (完整标志) 与 glyph.svg (小尺寸符号) 生成全部图标; ico 由 build-ico.py 打包。
// 两个 SVG 从 czl-skill 的 references/product-logo/marks/czlterm.svg 与 glyphs/czlterm.svg 拷贝,
// 是本仓库出图的单一来源; 改造型先改 czl-skill 再拷回来, 不在本仓库改。
//
//   npm i --prefix <scratchpad>/node --no-save @resvg/resvg-js@2.6.2
//   NODE_PATH=<scratchpad>/node/node_modules node docs/design/brand/build-icons.js
//   python docs/design/brand/build-ico.py [预览图输出路径]
//
// 中间帧写到 docs/design/brand/.frames/ (不提交), build-ico.py 从那里取。
const fs = require("fs");
const path = require("path");
const { Resvg } = require("@resvg/resvg-js");

const BRAND = __dirname;
const ROOT = path.resolve(BRAND, "../../..");
const DESKTOP = path.join(ROOT, "desktop");
const FRAMES = path.join(BRAND, ".frames");
fs.mkdirSync(FRAMES, { recursive: true });

function load(name) {
  const svg = fs.readFileSync(path.join(BRAND, name), "utf8");
  const [vx, vy, vw, vh] = svg.match(/viewBox="([^"]+)"/)[1].trim().split(/[\s,]+/).map(Number);
  const inner = svg.replace(/^[\s\S]*?<svg[^>]*>/, "").replace(/<\/svg>\s*$/, "");
  return { vx, vy, vw, vh, inner };
}

const mark = load("mark.svg");
const glyph = load("glyph.svg");

// 把图形放进 size 画布, 长边占 ratio, 水平垂直居中。recolor 把所有填充换成单色 (template / 白色版)。
function fit(src, size, ratio, recolor) {
  const s = (size * ratio) / Math.max(src.vw, src.vh);
  const x = (size - src.vw * s) / 2;
  const y = (size - src.vh * s) / 2;
  let inner = src.inner;
  if (recolor) inner = inner.replace(/<defs>[\s\S]*?<\/defs>/, "").replace(/fill="[^"]*"/g, `fill="${recolor}"`);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}"><g transform="translate(${x} ${y}) scale(${s}) translate(${-src.vx} ${-src.vy})">${inner}</g></svg>`;
}

const MARK_RATIO = 0.92; // 透明底完整标志长边占画布
const GLYPH_RATIO = 0.96; // 小尺寸符号撑满画布, 只留抗锯齿余量

function png(svg, out) {
  fs.mkdirSync(path.dirname(out), { recursive: true });
  fs.writeFileSync(out, new Resvg(svg).render().asPng());
}

// 留档预览
png(fit(mark, 512, MARK_RATIO), path.join(BRAND, "app-icon-512.png"));
png(fit(glyph, 256, GLYPH_RATIO), path.join(BRAND, "glyph-256.png"));

// macOS 应用图标: 1024 透明底完整标志, Wails 由它生成 icns。
png(fit(mark, 1024, MARK_RATIO), path.join(DESKTOP, "build", "appicon.png"));

// macOS 菜单栏: template 单色 (纯黑 + alpha, 系统按菜单栏主题着色), 18pt 的 1x / 2x。
png(fit(glyph, 18, GLYPH_RATIO, "#000000"), path.join(DESKTOP, "build", "tray", "template.png"));
png(fit(glyph, 36, GLYPH_RATIO, "#000000"), path.join(DESKTOP, "build", "tray", "template@2x.png"));

// 前端: 「关于」页 48px 图标用完整标志, WebView favicon 用小尺寸符号。
png(fit(mark, 256, MARK_RATIO), path.join(DESKTOP, "frontend", "public", "logo.png"));
png(fit(glyph, 256, GLYPH_RATIO), path.join(DESKTOP, "frontend", "app", "icon.png"));

// ico 帧: 16 / 20 / 24 / 32 小尺寸符号, 48 / 64 / 256 透明底完整标志。
for (const s of [16, 20, 24, 32]) png(fit(glyph, s, GLYPH_RATIO), path.join(FRAMES, `glyph-${s}.png`));
for (const s of [48, 64, 256]) png(fit(mark, s, MARK_RATIO), path.join(FRAMES, `mark-${s}.png`));

// 验收预览: 菜单栏 template 着色后的白色版
for (const s of [16, 24, 32]) png(fit(glyph, s, GLYPH_RATIO, "#FFFFFF"), path.join(FRAMES, `glyph-white-${s}.png`));

console.log("done");
