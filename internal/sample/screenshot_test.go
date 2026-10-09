package sample

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestSampleScreenshotRedactsWithoutChangingSource(t *testing.T) {
	source := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			source.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, source); err != nil {
		t.Fatal(err)
	}
	raw := append([]byte{}, buffer.Bytes()...)
	masked, err := RedactPNG(raw, []Mask{{X: 0.2, Y: 0.2, Width: 0.3, Height: 0.3}})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := png.Decode(bytes.NewReader(masked))
	if err != nil {
		t.Fatal(err)
	}
	r, _, _, _ := decoded.At(2, 2).RGBA()
	if r != 0 {
		t.Fatal("sensitive pixels remain")
	}
	r, _, _, _ = decoded.At(0, 0).RGBA()
	if r == 0 {
		t.Fatal("unmasked pixels lost")
	}
	if !bytes.Equal(raw, buffer.Bytes()) {
		t.Fatal("source altered")
	}
	if _, err = RedactPNG(raw, []Mask{{X: 0.8, Y: 0, Width: 0.3, Height: 1}}); err == nil {
		t.Fatal("out-of-bounds mask accepted")
	}
	if _, err = RedactPNG([]byte("not an image"), nil); err == nil {
		t.Fatal("non-PNG accepted")
	}
}
