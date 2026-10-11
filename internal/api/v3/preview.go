package v3

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"time"

	"github.com/nekoimi/scrapio/internal/drission_rod"
)

// Length-prefixed HTTP streaming keeps the existing Authorization header.
// Every packet is uint32(header bytes), uint32(image bytes), JSON header, JPEG.
// Preview is read-only and never updates the authoritative frame in PostgreSQL.
func StreamBrowserPreview(browser *drission_rod.DrissionRod) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admin, row, loaded := loadOwnedSession(w, r)
		if !loaded {
			return
		}
		if browser == nil {
			fail(w, r, 503, "STREAM_UNAVAILABLE", "浏览器流式画面不可用", true, "preview", "")
			return
		}
		latest, err := refreshOwnedSessionFromBrowser(r, admin.Id, row.Id, browser)
		if err != nil || latest == nil || terminalSession(latest.Status) || latest.Status != "ready" || !latest.ExpiresAt.After(time.Now()) {
			fail(w, r, 410, "SESSION_GONE", "会话不可用", false, "preview", "")
			return
		}
		if latest.CredentialRef != "" {
			fail(w, r, 409, "STREAM_MASK_REQUIRED", "授权会话使用已遮罩截图", false, "preview", "")
			return
		}
		if !supportsFlush(w) {
			fail(w, r, 503, "STREAM_UNAVAILABLE", "当前代理不支持画面流", false, "preview", "")
			return
		}
		controller := http.NewResponseController(w)
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		// A stalled browser may emit no frames; ownership/credential checks must still run.
		go func() {
			ticker := time.NewTicker(3 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					current, e := refreshOwnedSessionFromBrowser(r.WithContext(ctx), admin.Id, row.Id, browser)
					if e != nil || current == nil || current.Status != "ready" || !current.ExpiresAt.After(time.Now()) || current.CredentialRef != "" {
						cancel()
						return
					}
				}
			}
		}()
		w.Header().Set("Content-Type", "application/x-scrapio-preview")
		w.Header().Set("Cache-Control", "no-store, no-transform")
		w.Header().Set("X-Accel-Buffering", "no")
		writePacket := func(header any, image []byte) error {
			raw, e := json.Marshal(header)
			if e != nil {
				return e
			}
			var sizes [8]byte
			binary.BigEndian.PutUint32(sizes[:4], uint32(len(raw)))
			binary.BigEndian.PutUint32(sizes[4:], uint32(len(image)))
			_ = controller.SetWriteDeadline(time.Now().Add(5 * time.Second))
			for _, part := range [][]byte{sizes[:], raw, image} {
				if _, e = w.Write(part); e != nil {
					return e
				}
			}
			return controller.Flush()
		}
		err = browser.StreamEditorPreview(ctx, row.Id, func(metadata drission_rod.PreviewMetadata, image []byte) error { return writePacket(metadata, image) })
		if ctx.Err() == nil && err != nil {
			_ = writePacket(map[string]any{"protocol": drission_rod.EditorPreviewProtocol, "error_code": browserCode(err)}, nil)
		}
	}
}
