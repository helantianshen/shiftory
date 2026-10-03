package imageai

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"testing"
)

func TestPrepareImageUsesFirstGIFFrame(t *testing.T) {
	palette := color.Palette{color.Black, color.White}
	first := image.NewPaletted(image.Rect(0, 0, 2, 3), palette)
	second := image.NewPaletted(image.Rect(0, 0, 2, 3), palette)
	second.SetColorIndex(0, 0, 1)
	var encoded bytes.Buffer
	if err := gif.EncodeAll(&encoded, &gif.GIF{Image: []*image.Paletted{first, second}, Delay: []int{1, 1}}); err != nil {
		t.Fatal(err)
	}
	result, format, err := PrepareImage(encoded.Bytes())
	if err != nil || format != "png" {
		t.Fatal(format, err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(result))
	if err != nil {
		t.Fatal(err)
	}
	r, g, b, _ := decoded.At(0, 0).RGBA()
	if r != 0 || g != 0 || b != 0 {
		t.Fatal("GIF second frame used")
	}
}

func TestPrepareImageAppliesJPEGOrientation(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 2, 3))
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, source, nil); err != nil {
		t.Fatal(err)
	}
	for orientation := 1; orientation <= 8; orientation++ {
		tiff := make([]byte, 26)
		copy(tiff, "II")
		binary.LittleEndian.PutUint16(tiff[2:], 42)
		binary.LittleEndian.PutUint32(tiff[4:], 8)
		binary.LittleEndian.PutUint16(tiff[8:], 1)
		binary.LittleEndian.PutUint16(tiff[10:], 0x112)
		binary.LittleEndian.PutUint16(tiff[12:], 3)
		binary.LittleEndian.PutUint32(tiff[14:], 1)
		binary.LittleEndian.PutUint16(tiff[18:], uint16(orientation))
		body := append([]byte("Exif\x00\x00"), tiff...)
		header := []byte{0xff, 0xe1, 0, 0}
		binary.BigEndian.PutUint16(header[2:], uint16(len(body)+2))
		input := append([]byte{}, encoded.Bytes()[:2]...)
		input = append(input, header...)
		input = append(input, body...)
		input = append(input, encoded.Bytes()[2:]...)
		result, _, err := PrepareImage(input)
		if err != nil {
			t.Fatal(err)
		}
		conf, _, err := image.DecodeConfig(bytes.NewReader(result))
		if err != nil {
			t.Fatal(err)
		}
		w, h := 2, 3
		if orientation >= 5 {
			w, h = h, w
		}
		if conf.Width != w || conf.Height != h {
			t.Fatalf("orientation %d: %dx%d", orientation, conf.Width, conf.Height)
		}
	}
}
