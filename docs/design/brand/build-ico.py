"""把 build-icons.js 输出的帧手动打包成 ico, 并拼一张验收预览图。

    python docs/design/brand/build-ico.py [预览图输出路径]

  desktop/build/windows/icon.ico  应用图标: 16 / 24 / 32 小尺寸符号, 48 / 64 / 256 透明底完整标志
  desktop/build/windows/tray.ico  托盘: 16 / 20 / 24 / 32 小尺寸符号 (覆盖 100%-200% 缩放)

不让 Wails 从 appicon.png 缩放生成 ico: 缩放出来的 16 / 32 是整块标志, 括弧挤掉符号, 托盘里认不出。
"""

import struct
import sys
from io import BytesIO
from pathlib import Path

from PIL import Image

BRAND = Path(__file__).resolve().parent
ROOT = BRAND.parents[2]
FRAMES = BRAND / ".frames"
WIN = ROOT / "desktop" / "build" / "windows"


def pack_ico(frames: list[Path], out: Path) -> None:
    """每帧以 PNG 原样写进 ico (Vista 起支持), 不经 Pillow 重新缩放。"""
    blobs = []
    for f in frames:
        im = Image.open(f).convert("RGBA")
        buf = BytesIO()
        im.save(buf, "PNG")
        blobs.append((im.width, buf.getvalue()))
    header = struct.pack("<HHH", 0, 1, len(blobs))
    offset = 6 + 16 * len(blobs)
    entries, data = b"", b""
    for size, blob in blobs:
        dim = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(blob), offset + len(data))
        data += blob
    out.write_bytes(header + entries + data)


def preview(out: Path) -> None:
    """48px 完整标志放在深色壁纸 / 中灰 / 浅色底; 16 / 24 / 32 符号原尺寸与 4 倍放大放在亮 / 暗任务栏底。"""
    W = 760
    canvas = Image.new("RGB", (W, 520), (200, 200, 200))
    for i, bg in enumerate([(24, 30, 48), (128, 128, 128), (243, 243, 243)]):
        canvas.paste(Image.new("RGB", (W // 3, 120), bg), (i * (W // 3), 0))
        m = Image.open(FRAMES / "mark-48.png")
        canvas.paste(m, (i * (W // 3) + 30, 36), m)
        big = Image.open(FRAMES / "mark-256.png").resize((96, 96), Image.LANCZOS)
        canvas.paste(big, (i * (W // 3) + 120, 12), big)
    y = 120
    for bg in [(243, 243, 243), (32, 32, 32)]:
        band = Image.new("RGB", (W, 200), bg)
        x = 16
        sets = ["glyph"] + (["glyph-white"] if bg[0] < 128 else [])
        for prefix in sets:
            for s in (16, 24, 32):
                g = Image.open(FRAMES / f"{prefix}-{s}.png")
                band.paste(g, (x, 16), g)
                big = g.resize((s * 4, s * 4), Image.NEAREST)
                band.paste(big, (x, 56), big)
                x += s * 4 + 24
        canvas.paste(band, (0, y))
        y += 200
    canvas.save(out)


def main() -> None:
    pack_ico(
        [FRAMES / f"glyph-{s}.png" for s in (16, 24, 32)] + [FRAMES / f"mark-{s}.png" for s in (48, 64, 256)],
        WIN / "icon.ico",
    )
    pack_ico([FRAMES / f"glyph-{s}.png" for s in (16, 20, 24, 32)], WIN / "tray.ico")
    if len(sys.argv) > 1:
        preview(Path(sys.argv[1]))
    print("ico done")


if __name__ == "__main__":
    main()
