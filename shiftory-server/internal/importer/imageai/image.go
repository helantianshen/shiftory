package imageai

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
)

func PrepareImage(content []byte) ([]byte, string, error) {
	info, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil || info.Width <= 0 || info.Height <= 0 || int64(info.Width)*int64(info.Height) > 25_000_000 {
		return nil, "", errors.New("invalid image")
	}
	orientation := 1
	if format == "jpeg" {
		orientation = jpegOrientation(content)
	}
	if format != "gif" && orientation == 1 {
		return content, format, nil
	}
	source, _, err := image.Decode(bytes.NewReader(content))
	if err != nil {
		return nil, "", errors.New("invalid image data")
	}
	bounds := source.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	dw, dh := w, h
	if orientation >= 5 {
		dw, dh = h, w
	}
	target := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = w - 1 - x
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dy = h - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			target.Set(dx, dy, source.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	var out bytes.Buffer
	if err = png.Encode(&out, target); err != nil {
		return nil, "", err
	}
	return out.Bytes(), "png", nil
}
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for pos := 2; pos+4 <= len(data); {
		if data[pos] != 0xff {
			return 1
		}
		marker := data[pos+1]
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		length := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if length < 2 || pos+2+length > len(data) {
			return 1
		}
		body := data[pos+4 : pos+2+length]
		pos += length + 2
		if marker != 0xe1 || len(body) < 14 || string(body[:6]) != "Exif\x00\x00" {
			continue
		}
		tiff := body[6:]
		var order binary.ByteOrder = binary.LittleEndian
		if string(tiff[:2]) == "MM" {
			order = binary.BigEndian
		} else if string(tiff[:2]) != "II" {
			return 1
		}
		if order.Uint16(tiff[2:4]) != 42 {
			return 1
		}
		offset := int(order.Uint32(tiff[4:8]))
		if offset < 8 || offset+2 > len(tiff) {
			return 1
		}
		count := int(order.Uint16(tiff[offset : offset+2]))
		for i := 0; i < count; i++ {
			p := offset + 2 + i*12
			if p+12 > len(tiff) {
				return 1
			}
			if order.Uint16(tiff[p:p+2]) == 0x112 && order.Uint16(tiff[p+2:p+4]) == 3 && order.Uint32(tiff[p+4:p+8]) == 1 {
				v := int(order.Uint16(tiff[p+8 : p+10]))
				if v >= 1 && v <= 8 {
					return v
				}
				return 1
			}
		}
	}
	return 1
}
