// 一次性脚本: 拼装小尺寸符号 glyph.json (粗壮的终端提示符 `>_`, 两笔圆头)。
// glyph.json 才是 ≤32px 图标的单一来源; 改造型时改这里的几何参数再跑一次。
//
//   node docs/design/brand/compose-glyph.js
//
// 每一笔是一条圆头胶囊 (两段直边 + 两个半圆), 同向绕行, 用 nonzero 填充合并;
// `>` 两臂在顶点重叠, 圆头自然形成圆角拐点。笔宽在 16px 下约 3 个像素。
const fs = require("fs");
const path = require("path");

const R = 25; // 半笔宽
const strokes = [
  [[0, 0], [92, 84]], // `>` 上臂
  [[92, 84], [0, 168]], // `>` 下臂
  [[124, 168], [206, 168]], // `_`
];

function capsule([ax, ay], [bx, by]) {
  const len = Math.hypot(bx - ax, by - ay);
  const nx = (-(by - ay) / len) * R;
  const ny = ((bx - ax) / len) * R;
  const f = (x, y) => `${x.toFixed(2)} ${y.toFixed(2)}`;
  return (
    `M${f(ax + nx, ay + ny)}L${f(bx + nx, by + ny)}` +
    `A${R} ${R} 0 0 0 ${f(bx - nx, by - ny)}L${f(ax - nx, ay - ny)}` +
    `A${R} ${R} 0 0 0 ${f(ax + nx, ay + ny)}Z`
  );
}

// 平移到包围盒原点: 所有端点外扩半笔宽。
const xs = strokes.flat().map((p) => p[0]);
const ys = strokes.flat().map((p) => p[1]);
const x0 = Math.min(...xs) - R;
const y0 = Math.min(...ys) - R;
const w = Math.max(...xs) + R - x0;
const h = Math.max(...ys) + R - y0;
const d = strokes
  .map(([a, b]) => capsule([a[0] - x0, a[1] - y0], [b[0] - x0, b[1] - y0]))
  .join("");

fs.writeFileSync(path.join(__dirname, "glyph.json"), JSON.stringify({ d, w, h }));
console.log(`glyph ${w} x ${h}`);
