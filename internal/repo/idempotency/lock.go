package idempotency

import (
	"fmt"
	"xorm.io/xorm"
)

// Lock serializes matching requests across connections until transaction completion.
func Lock(s *xorm.Session, ownerID int64, operation, key string) error {
	if key == "" {
		return nil
	}
	_, err := s.QueryString("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", fmt.Sprintf("%d:%s:%s", ownerID, operation, key))
	return err
}
