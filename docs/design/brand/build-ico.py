"""把 build-icons.js 输出的帧手动打包成 ico, 并拼一张验收预览图。

    python docs/design/brand/build-ico.py [预览图输出路径]

  desktop/build/windows/icon.ico  应用图标: 16 / 24 / 32 小尺寸符号, 48 / 64 / 256 白色圆角底板 + 渐变标志
  desktop/build/windows/tray.ico  托盘: 16 / 20 / 24 / 32 小尺寸符号 (覆盖 100%-200% 缩放)

不让 Wails 从 appicon.png 缩放生成 ico: 缩放出来的 16 / 32 是整块标志, 托盘里认不出。
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
    """小尺寸符号 16 / 24 / 32 原尺寸与 4 倍放大, 亮 / 暗任务栏底色; 下方是应用图标 60 / 180。"""
    bgs = [(243, 243, 243), (32, 32, 32)]
    W, H = 760, 520
    canvas = Image.new("RGB", (W, H), (200, 200, 200))
    y = 0
    for bg in bgs:
        band = Image.new("RGB", (W, 180), bg)
        x = 16
        for s in (16, 24, 32):
            g = Image.open(FRAMES / f"glyph-{s}.png")
            band.paste(g, (x, 20), g)
            big = g.resize((s * 4, s * 4), Image.NEAREST)
            band.paste(big, (x, 52), big)
            x += s * 4 + 32
        # 暗底再放一份白色版 (macOS 菜单栏 template 着色后的效果)
        if bg[0] < 128:
            for s in (16, 24, 32):
                g = Image.open(FRAMES / f"glyph-white-{s}.png")
                band.paste(g, (x, 20), g)
                big = g.resize((s * 4, s * 4), Image.NEAREST)
                band.paste(big, (x, 52), big)
                x += s * 4 + 32
        canvas.paste(band, (0, y))
        y += 180
    for i, name in enumerate(("plate-60.png", "plate-180.png")):
        im = Image.open(FRAMES / name)
        for j, bg in enumerate(bgs):
            canvas.paste(Image.new("RGB", (220, 160), bg), (20 + j * 240 + i * 0, 360))
    for j, bg in enumerate(bgs):
        a = Image.open(FRAMES / "plate-60.png")
        b = Image.open(FRAMES / "plate-180.png").resize((140, 140), Image.LANCZOS)
        canvas.paste(a, (30 + j * 240, 410), a)
        canvas.paste(b, (100 + j * 240, 370), b)
    canvas.save(out)


def main() -> None:
    pack_ico(
        [FRAMES / f"glyph-{s}.png" for s in (16, 24, 32)] + [FRAMES / f"plate-{s}.png" for s in (48, 64, 256)],
        WIN / "icon.ico",
    )
    pack_ico([FRAMES / f"glyph-{s}.png" for s in (16, 20, 24, 32)], WIN / "tray.ico")
    if len(sys.argv) > 1:
        preview(Path(sys.argv[1]))
    print("ico done")


if __name__ == "__main__":
    main()
