//go:build windows

package app

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"unsafe"
)

const (
	cfDIB  = 8
	cfDIB5 = 17

	biRGB       = 0
	biBitfields = 3

	maxImageBytes = 64 << 20 // 64 MB，够 4K 截图了
)

// GetClipboardPNG 如果剪贴板里是位图，就转成 PNG 返回；不是位图返回 (nil, nil)。
func GetClipboardPNG() ([]byte, error) {
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfDIB); r != 0 {
		return readClipboardImage(cfDIB)
	}
	if r, _, _ := procIsClipboardFormatAvailable.Call(cfDIB5); r != 0 {
		return readClipboardImage(cfDIB5)
	}
	return nil, nil
}

func readClipboardImage(format uintptr) ([]byte, error) {
	if err := openClipboard(); err != nil {
		return nil, err
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(format)
	if h == 0 {
		return nil, nil
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil, errors.New("无法锁定剪贴板图像内存")
	}
	defer procGlobalUnlock.Call(h)

	size, _, _ := procGlobalSize.Call(h)
	if size < 40 || size > maxImageBytes {
		return nil, nil
	}
	raw := unsafe.Slice((*byte)(unsafe.Pointer(p)), int(size))
	buf := make([]byte, len(raw))
	copy(buf, raw) // 关掉剪贴板后这块内存就不再归我们了
	return dibToPNG(buf)
}

// dibToPNG 把剪贴板里的 DIB（BITMAPINFOHEADER + 像素）转成 PNG。
// 只处理最常见的 BI_RGB / BI_BITFIELDS + 24/32 位，其他格式直接放弃。
func dibToPNG(b []byte) ([]byte, error) {
	if len(b) < 40 {
		return nil, errors.New("位图头不完整")
	}
	headerSize := int(binary.LittleEndian.Uint32(b[0:4]))
	if headerSize < 40 || headerSize > len(b) {
		return nil, errors.New("位图头大小异常")
	}

	w := int(int32(binary.LittleEndian.Uint32(b[4:8])))
	rawH := int(int32(binary.LittleEndian.Uint32(b[8:12])))
	bitCount := int(binary.LittleEndian.Uint16(b[14:16]))
	compression := binary.LittleEndian.Uint32(b[16:20])

	if w <= 0 || rawH == 0 || w > 20000 || rawH > 20000 || rawH < -20000 {
		return nil, errors.New("位图尺寸异常")
	}
	topDown := rawH < 0
	h := rawH
	if topDown {
		h = -h
	}
	if bitCount != 24 && bitCount != 32 {
		return nil, errors.New("只支持 24 位或 32 位位图")
	}
	if compression != biRGB && compression != biBitfields {
		return nil, errors.New("不支持的位图压缩方式")
	}

	offset := headerSize
	if compression == biBitfields {
		offset += 12 // 三个颜色掩码
	}
	stride := ((w*bitCount + 31) / 32) * 4
	if offset >= len(b) {
		return nil, errors.New("缺少像素数据")
	}
	// 有些程序给的 GlobalSize 比实际小，按能读到的行数来
	rows := (len(b) - offset) / stride
	if rows <= 0 {
		return nil, errors.New("像素数据不完整")
	}
	usable := h
	if rows < usable {
		usable = rows
	}

	img := image.NewRGBA(image.Rect(0, 0, w, usable))
	anyAlpha := false
	for y := 0; y < usable; y++ {
		srcY := y
		if !topDown {
			srcY = h - 1 - y // 自下而上存储
		}
		if srcY >= rows {
			break
		}
		row := b[offset+srcY*stride:]
		for x := 0; x < w; x++ {
			var r, g, bl, a byte
			if bitCount == 32 {
				p := row[x*4 : x*4+4]
				bl, g, r, a = p[0], p[1], p[2], p[3]
				if a != 0 {
					anyAlpha = true
				}
			} else {
				p := row[x*3 : x*3+3]
				bl, g, r, a = p[0], p[1], p[2], 255
			}
			img.SetRGBA(x, y, color.RGBA{R: r, G: g, B: bl, A: a})
		}
	}

	// 32 位位图的 alpha 经常整块是 0（比如 QQ/微信截图），当成不透明才对
	if bitCount == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
