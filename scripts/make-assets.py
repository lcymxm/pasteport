"""生成 README 用的图标和社交预览图（项目自己的素材，纯 Pillow 绘制）。

    python scripts/make-assets.py

输出：
    docs/icon.png                  1024x1024 应用图标
    docs/social-preview.png        1280x640  GitHub 社交预览
    android/res/mipmap-*/ic_launcher.png
"""
import os
import numpy as np
from PIL import Image, ImageDraw, ImageFilter, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
S = 2048

TOP = (94, 234, 212)     # #5EEAD4
BOT = (20, 150, 176)
INK = (4, 35, 31)        # #04231F
BG = (10, 12, 15)        # #0A0C0F

FONT_BOLD = r"C:\Windows\Fonts\segoeuib.ttf"
FONT_CN = r"C:\Windows\Fonts\msyh.ttc"
FONT_MONO = r"C:\Windows\Fonts\consola.ttf"


def gradient(size, top, bot):
    yy, xx = np.mgrid[0:size, 0:size].astype(np.float32)
    t = np.clip((xx + yy) / (2.0 * (size - 1)), 0, 1)[..., None]
    arr = (1 - t) * np.array(top, dtype=np.float32) + t * np.array(bot, dtype=np.float32)
    return Image.fromarray(arr.astype(np.uint8), "RGB")


def rounded_mask(size, radius_ratio=0.225):
    mask = Image.new("L", (size, size), 0)
    ImageDraw.Draw(mask).rounded_rectangle(
        [0, 0, size - 1, size - 1], radius=int(size * radius_ratio), fill=255)
    return mask


def arrow_layer(size):
    layer = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(layer)
    w = int(size * 0.058)
    y1, y2 = int(size * 0.385), int(size * 0.615)
    xl, xr = int(size * 0.255), int(size * 0.745)
    hl, hh = int(size * 0.165), int(size * 0.118)

    d.line([(xl, y1), (xr - hl * 0.65, y1)], fill=INK, width=w)
    d.polygon([(xr, y1), (xr - hl, y1 - hh), (xr - hl, y1 + hh)], fill=INK)
    for cx in (xl, int(xr - hl * 0.65)):
        d.ellipse([cx - w // 2, y1 - w // 2, cx + w // 2, y1 + w // 2], fill=INK)

    d.line([(xr, y2), (xl + hl * 0.65, y2)], fill=INK, width=w)
    d.polygon([(xl, y2), (xl + hl, y2 - hh), (xl + hl, y2 + hh)], fill=INK)
    for cx in (xr, int(xl + hl * 0.65)):
        d.ellipse([cx - w // 2, y2 - w // 2, cx + w // 2, y2 + w // 2], fill=INK)
    return layer


def build_icon():
    icon = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    icon.paste(gradient(S, TOP, BOT).convert("RGBA"), (0, 0), rounded_mask(S))
    edge = Image.new("RGBA", (S, S), (0, 0, 0, 0))
    ImageDraw.Draw(edge).rounded_rectangle(
        [6, 6, S - 7, S - 7], radius=int(S * 0.225), outline=(255, 255, 255, 46), width=8)
    icon = Image.alpha_composite(icon, edge)
    return Image.alpha_composite(icon, arrow_layer(S))


def save(icon, rel, px):
    path = os.path.join(ROOT, rel)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    icon.resize((px, px), Image.LANCZOS).save(path, optimize=True)
    print(f"  {rel:<52} {px}x{px}")


def font(path, size, index=0):
    try:
        return ImageFont.truetype(path, size, index=index)
    except Exception:
        return ImageFont.load_default()


def build_social(icon):
    W, H = 1280, 640
    canvas = Image.new("RGB", (W, H), BG)
    glow = Image.new("RGB", (W, H), BG)
    gd = ImageDraw.Draw(glow)
    gd.ellipse([-260, -420, 720, 420], fill=(16, 46, 44))
    gd.ellipse([820, -300, 1560, 380], fill=(14, 32, 52))
    canvas = Image.blend(canvas, glow.filter(ImageFilter.GaussianBlur(120)), 0.65)

    big = icon.resize((176, 176), Image.LANCZOS)
    canvas.paste(big, (96, 96), big)

    title = font(FONT_BOLD, 78)
    sub = font(FONT_CN, 34)
    chip_font = font(FONT_CN, 25)
    mono = font(FONT_MONO, 24)

    d = ImageDraw.Draw(canvas)
    d.text((96, 310), "PastePort", font=title, fill=(240, 248, 250))
    d.text((100, 410), "电脑复制，手机粘贴 · 局域网直连", font=sub, fill=(150, 168, 180))

    x = 100
    for text in ["免安装", "无账号", "不过云", "exe 7 MB", "apk 49 KB"]:
        tw = d.textlength(text, font=chip_font)
        d.rounded_rectangle([x, 480, x + tw + 34, 532], radius=26,
                            fill=(18, 30, 36), outline=(35, 60, 62))
        d.text((x + 17, 493), text, font=chip_font, fill=(94, 234, 212))
        x += tw + 50

    d.text((96, 574), "github.com/<you>/pasteport", font=mono, fill=(90, 106, 118))
    path = os.path.join(ROOT, "docs", "social-preview.png")
    canvas.save(path, optimize=True)
    print("  docs/social-preview.png                              1280x640")


if __name__ == "__main__":
    print("图标：")
    icon = build_icon()
    save(icon, "docs/icon.png", 1024)
    for folder, px in [("mdpi", 48), ("hdpi", 72), ("xhdpi", 96), ("xxhdpi", 144), ("xxxhdpi", 192)]:
        save(icon, f"android/res/mipmap-{folder}/ic_launcher.png", px)
    print("社交预览：")
    build_social(icon)
