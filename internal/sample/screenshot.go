package sample

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
)

// Mask uses fractions of the image dimensions, independent of display scaling.
type Mask struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

func ValidateMasks(masks []Mask) error {
	if len(masks) > 50 {
		return errors.New("at most 50 screenshot masks")
	}
	for _, m := range masks {
		for _, v := range []float64{m.X, m.Y, m.Width, m.Height} {
			if math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("invalid mask coordinates")
			}
		}
		if m.X < 0 || m.Y < 0 || m.Width <= 0 || m.Height <= 0 || m.X+m.Width > 1.000001 || m.Y+m.Height > 1.000001 {
			return errors.New("mask must be inside screenshot")
		}
	}
	return nil
}

// RedactPNG decodes and re-encodes pixels, removes metadata, and only returns
// the masked image. The original screenshot is never persisted by the caller.
func RedactPNG(raw []byte, masks []Mask) ([]byte, error) {
	if err := ValidateMasks(masks); err != nil {
		return nil, err
	}
	if len(raw) == 0 || len(raw) > 2*1024*1024 {
		return nil, errors.New("screenshot exceeds 2 MiB")
	}
	config, err := png.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 8000000 {
		return nil, errors.New("invalid PNG or screenshot pixel budget exceeded")
	}
	source, err := png.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, errors.New("invalid PNG")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(canvas, canvas.Bounds(), source, source.Bounds().Min, draw.Src)
	for _, m := range masks {
		rect := image.Rect(int(math.Floor(m.X*float64(config.Width))), int(math.Floor(m.Y*float64(config.Height))), int(math.Ceil((m.X+m.Width)*float64(config.Width))), int(math.Ceil((m.Y+m.Height)*float64(config.Height)))).Intersect(canvas.Bounds())
		draw.Draw(canvas, rect, &image.Uniform{C: color.Black}, image.Point{}, draw.Src)
	}
	var out bytes.Buffer
	if err = png.Encode(&out, canvas); err != nil {
		return nil, err
	}
	if out.Len() > 2*1024*1024 {
		return nil, errors.New("masked PNG exceeds 2 MiB")
	}
	return out.Bytes(), nil
}
