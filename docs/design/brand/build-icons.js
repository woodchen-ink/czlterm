// 由 mark.json (完整标志) 与 glyph.json (小尺寸符号) 生成全部图标; ico 由 build-ico.py 打包。
//
//   npm i --prefix <scratchpad>/node --no-save @resvg/resvg-js@2.6.2
//   NODE_PATH=<scratchpad>/node/node_modules node docs/design/brand/build-icons.js
//   python docs/design/brand/build-ico.py
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

const mark = JSON.parse(fs.readFileSync(path.join(BRAND, "mark.json"), "utf8"));
const glyph = JSON.parse(fs.readFileSync(path.join(BRAND, "glyph.json"), "utf8"));

const C1 = "#2EA7E0";
const C2 = "#1D2088";
const RADIUS = 0.2237; // 圆角 / 底板边长
const MARK_RATIO = 0.78; // 标志宽 / 底板边长

const gradient = `<defs><linearGradient id="g" x1="0" y1="0" x2="1" y2="0"><stop offset="0" stop-color="${C1}"/><stop offset="1" stop-color="${C2}"/></linearGradient></defs>`;

// 标志放进 (x, y, w) 区域内水平居中, 垂直以 cy 为中心。
function markAt(cx, cy, width, fill) {
  const s = width / mark.w;
  const x = cx - (mark.w * s) / 2;
  const y = cy - (mark.h * s) / 2;
  return `<path transform="translate(${x} ${y}) scale(${s})" fill="${fill}" fill-rule="evenodd" d="${mark.d}"/>`;
}

// 白色圆角底板 + 渐变标志。inset: 底板距画布边; stroke: 是否加极浅灰描边 (Windows 无系统投影)。
function plateSvg(size, { inset = 0, stroke = false, full = false } = {}) {
  const side = size - inset * 2;
  const rx = full ? 0 : side * RADIUS;
  const sw = stroke ? Math.max(1, size / 128) : 0;
  const plate = `<rect x="${inset + sw / 2}" y="${inset + sw / 2}" width="${side - sw}" height="${side - sw}" rx="${rx}" fill="#FFFFFF"${stroke ? ` stroke="#D9DEE6" stroke-width="${sw}"` : ""}/>`;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}">${gradient}${plate}${markAt(size / 2, size / 2, side * MARK_RATIO, "url(#g)")}</svg>`;
}

// 小尺寸符号撑满画布, 四周只留抗锯齿余量。
function glyphSvg(size, fill, pad = 0.04) {
  const avail = size * (1 - pad * 2);
  const s = Math.min(avail / glyph.w, avail / glyph.h);
  const x = (size - glyph.w * s) / 2;
  const y = (size - glyph.h * s) / 2;
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${size}" height="${size}" viewBox="0 0 ${size} ${size}"><path transform="translate(${x} ${y}) scale(${s})" fill="${fill}" d="${glyph.d}"/></svg>`;
}

function png(svg, out) {
  fs.mkdirSync(path.dirname(out), { recursive: true });
  fs.writeFileSync(out, new Resvg(svg).render().asPng());
}

// 矢量
fs.writeFileSync(
  path.join(BRAND, "mark.svg"),
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${mark.w} ${mark.h}">${gradient.replace('x2="1"', 'x2="1" gradientUnits="objectBoundingBox"')}<path fill="url(#g)" fill-rule="evenodd" d="${mark.d}"/></svg>\n`,
);
fs.writeFileSync(
  path.join(BRAND, "glyph.svg"),
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${glyph.w} ${glyph.h}"><path fill="${C1}" d="${glyph.d}"/></svg>\n`,
);
fs.writeFileSync(path.join(BRAND, "app-icon.svg"), plateSvg(1024, { full: true }) + "\n");

// 留档与样板
png(plateSvg(512), path.join(BRAND, "app-icon-512.png"));
png(glyphSvg(256, C1, 0.02), path.join(BRAND, "glyph-256.png"));

// macOS: Apple 图标网格, 1024 画布里 824 的圆角底板; 投影由系统加。Wails 由它生成 icns。
png(plateSvg(1024, { inset: 100 }), path.join(DESKTOP, "build", "appicon.png"));

// macOS 菜单栏: template 单色 (纯黑 + alpha), 18pt 的 1x / 2x。
png(glyphSvg(18, "#000000", 0.02), path.join(DESKTOP, "build", "tray", "template.png"));
png(glyphSvg(36, "#000000", 0.02), path.join(DESKTOP, "build", "tray", "template@2x.png"));

// 前端: 「关于」页图标与 WebView favicon。
png(plateSvg(256, { inset: 4, stroke: true }), path.join(DESKTOP, "frontend", "public", "logo.png"));
png(glyphSvg(256, C1, 0.02), path.join(DESKTOP, "frontend", "app", "icon.png"));

// ico 帧: 16 / 20 / 24 / 32 用小尺寸符号, 48 / 64 / 256 用白色圆角底板 (几乎铺满, 与其它应用等大)。
for (const s of [16, 20, 24, 32]) png(glyphSvg(s, C1), path.join(FRAMES, `glyph-${s}.png`));
for (const s of [48, 64, 256]) png(plateSvg(s, { inset: Math.round(s / 64), stroke: true }), path.join(FRAMES, `plate-${s}.png`));

// 验收预览帧 (4 倍放大由 build-ico.py 拼)
for (const s of [16, 24, 32]) png(glyphSvg(s, "#FFFFFF"), path.join(FRAMES, `glyph-white-${s}.png`));
png(plateSvg(60, { inset: 1, stroke: true }), path.join(FRAMES, "plate-60.png"));
png(plateSvg(180, { full: true }), path.join(FRAMES, "plate-180.png"));

console.log("done");
