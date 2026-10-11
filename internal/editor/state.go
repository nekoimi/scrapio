package editor

import (
	"errors"
	"math"
	"strings"
)

const StateProtocol = "editor.state.v1"

// State names separate kinds of evidence; PageStateID remains the mandatory
// conservative guard. A matching DOM token never authorizes an old coordinate.
type State struct {
	Protocol          string  `json:"protocol"`
	PageStateID       string  `json:"page_state_id"`
	DocumentID        string  `json:"document_id"`
	ValidationStateID string  `json:"validation_state_id"`
	FrameStateID      string  `json:"frame_state_id"`
	Viewport          Size    `json:"viewport"`
	Scroll            Offset  `json:"scroll"`
	PixelRatio        float64 `json:"pixel_ratio"`
	FrameSequence     int64   `json:"frame_sequence,omitempty"`
	Image             *Size   `json:"image,omitempty"`
	CapturedAtUnixMS  int64   `json:"captured_at_unix_ms,omitempty"`
}
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}
type Offset struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func (s *State) Validate(pageState string, frame bool) error {
	bad := errors.New("invalid editor state metadata")
	if s == nil || s.Protocol != StateProtocol || s.PageStateID != pageState || pageState == "" {
		return bad
	}
	for _, id := range []string{s.DocumentID, s.ValidationStateID, s.FrameStateID} {
		if len(id) != 64 || strings.IndexFunc(id, func(r rune) bool { return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') }) >= 0 {
			return bad
		}
	}
	if s.Viewport.Width < 1 || s.Viewport.Width > 16384 || s.Viewport.Height < 1 || s.Viewport.Height > 16384 || s.PixelRatio <= 0 || s.PixelRatio > 8 {
		return bad
	}
	for _, v := range []float64{s.PixelRatio, s.Scroll.X, s.Scroll.Y} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return bad
		}
	}
	if frame && (s.Image == nil || s.Image.Width < 1 || s.Image.Width > 16384 || s.Image.Height < 1 || s.Image.Height > 16384 || s.FrameSequence < 1 || s.FrameSequence > 9007199254740991 || s.CapturedAtUnixMS < 1) {
		return bad
	}
	return nil
}
