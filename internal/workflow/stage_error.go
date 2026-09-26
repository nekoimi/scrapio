package workflow

import "fmt"

// StageError keeps a machine-readable stage in the existing attempt snapshot.
type StageError struct {
	Stage          string
	Cause          error
	CandidateCount int
}

func (e *StageError) Error() string { return fmt.Sprintf("%s: %v", e.Stage, e.Cause) }
func (e *StageError) Unwrap() error { return e.Cause }
func (e *StageError) FailureSnapshot() any {
	return map[string]any{"stage": e.Stage, "candidate_count": e.CandidateCount}
}
