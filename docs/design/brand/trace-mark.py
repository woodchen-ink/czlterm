"""一次性脚本: 把历史 AI 位图 desktop/build/icons/logo.png 描摹成 mark.json。

mark.json 才是完整标志的单一来源; 这个脚本只记录它是怎么来的, 平时不用再跑。

    pip install --target <scratchpad>/py potracer
    PYTHONPATH=<scratchpad>/py python docs/design/brand/trace-mark.py

CZL Term 是规范所列历史变体 (条纹切开外框 + 中间提示符), 保持原构图, 只换成矢量。
"""

import json
from pathlib import Path

import numpy as np
import potrace
from PIL import Image

ROOT = Path(__file__).resolve().parents[3]
SRC = ROOT / "desktop" / "build" / "icons" / "logo.png"
OUT = Path(__file__).resolve().parent / "mark.json"
SCALE = 2  # 先放大再取阈值, 边缘更顺


def main() -> None:
    im = Image.open(SRC).convert("RGBA")
    im = im.resize((im.width * SCALE, im.height * SCALE), Image.LANCZOS)
    px = np.asarray(im).astype(np.int32)
    r, g, b, a = px[..., 0], px[..., 1], px[..., 2], px[..., 3]
    # 标志本体是蓝色; 源图有黑边与低 alpha 杂色, 中间提示符是白色 (镂空)。
    blue = (a >= 128) & (b >= 90) & (b - r >= 60) & ~((r > 170) & (g > 170))
    # potracer 内部会把输入反相, 传入的蒙版先取反。
    bmp = potrace.Bitmap(~blue)
    path = bmp.trace(turdsize=400, alphamax=0.9, opticurve=True, opttolerance=0.3)

    s = 1 / SCALE
    ys, xs = np.nonzero(blue)
    x0, y0 = xs.min() * s, ys.min() * s
    x1, y1 = (xs.max() + 1) * s, (ys.max() + 1) * s

    def pt(p):  # 平移到包围盒原点
        return f"{p.x*s-x0:.1f} {p.y*s-y0:.1f}"

    parts = []
    for curve in path:
        seg = [f"M{pt(curve.start_point)}"]
        for c in curve:
            if c.is_corner:
                seg.append(f"L{pt(c.c)}L{pt(c.end_point)}")
            else:
                seg.append(f"C{pt(c.c1)} {pt(c.c2)} {pt(c.end_point)}")
        seg.append("Z")
        parts.append("".join(seg))

    d = "".join(parts)
    OUT.write_text(
        json.dumps({"d": d, "w": round(x1 - x0, 1), "h": round(y1 - y0, 1)}),
        encoding="utf-8",
    )
    print(f"{len(path)} curves, bbox {x0},{y0} {x1},{y1}")


if __name__ == "__main__":
    main()
