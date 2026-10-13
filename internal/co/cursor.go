package co

import (
	"encoding/base64"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/nekoimi/scrapio/internal/collector"
	"time"
)

type cursor struct {
	At time.Time `json:"at"`
	ID string    `json:"id"`
}

func encodeCursor(at time.Time, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(collector.JSON(cursor{At: at, ID: id})))
}
func decodeCursor(raw string) (cursor, error) {
	var c cursor
	if len(raw) > 256 {
		return c, collector.Invalid("cursor", "分页游标无效")
	}
	data, e := base64.RawURLEncoding.DecodeString(raw)
	if e != nil || json.Unmarshal(data, &c) != nil || c.At.IsZero() {
		return c, collector.Invalid("cursor", "分页游标无效")
	}
	if _, e = uuid.Parse(c.ID); e != nil {
		return c, collector.Invalid("cursor", "分页游标无效")
	}
	return c, nil
}
