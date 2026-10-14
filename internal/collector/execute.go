package collector

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"
)

type Source interface {
	Open(context.Context, Request) (Document, error)
	Action(context.Context, Action, string) error
	Snapshot(context.Context) (Document, error)
	Close()
}
type Checkpoint struct {
	NextURL        string   `json:"next_url,omitempty"`
	Cursor         string   `json:"cursor,omitempty"`
	Resume         string   `json:"resume"`
	Pages          int      `json:"pages"`
	Records        int      `json:"records"`
	Done           bool     `json:"done"`
	FinalStatus    string   `json:"final_status,omitempty"`
	FinalReason    string   `json:"final_reason,omitempty"`
	SeenPages      []string `json:"seen_pages,omitempty"`
	CurrentURL     string   `json:"current_url,omitempty"`
	ContentHash    string   `json:"content_hash,omitempty"`
	EmptyVerified  bool     `json:"empty_verified,omitempty"`
	InputHash      string   `json:"input_hash"`
	DefinitionHash string   `json:"definition_hash"`
}
type PageEvidence struct {
	URL           string `json:"url"`
	ContentHash   string `json:"content_hash"`
	RecordCount   int    `json:"record_count"`
	EmptyVerified bool   `json:"empty_verified"`
}
type Result struct {
	Status             string         `json:"status"`
	Mode               string         `json:"mode"`
	Reason             string         `json:"reason,omitempty"`
	InterpreterVersion string         `json:"interpreter_version"`
	DefinitionHash     string         `json:"definition_hash"`
	Input              map[string]any `json:"input"`
	Candidates         []Candidate    `json:"candidates"`
	Pages              []PageEvidence `json:"pages"`
	Checkpoint         Checkpoint     `json:"checkpoint"`
	Warnings           []string       `json:"warnings"`
	Error              *Error         `json:"error,omitempty"`
	Snapshots          []Document     `json:"-"`
	Comparison         map[string]any `json:"comparison,omitempty"`
	FormalWrites       int            `json:"formal_writes"`
}

func AsError(e error) *Error {
	if x, ok := e.(*Error); ok {
		return x
	}
	return &Error{Code: "EXECUTION_FAILED", Message: "执行失败，请检查连接或页面状态", Retryable: false}
}

type DurableOptions struct {
	Checkpoint Checkpoint
	Commit     func(context.Context, []Candidate, Checkpoint) error
}

// Execute is shared by isolated tests and future O2 work items. It never saves business records.
func Execute(ctx context.Context, d Definition, input map[string]any, mode string, source Source) Result {
	return ExecuteDurable(ctx, d, input, mode, source, DurableOptions{})
}

// ExecuteDurable flushes each bounded page before opening its successor.
func ExecuteDurable(ctx context.Context, d Definition, input map[string]any, mode string, source Source, options DurableOptions) (result Result) {
	result = Result{Status: "failed", Mode: mode, InterpreterVersion: Interpreter, DefinitionHash: Hash(d), Input: map[string]any{}, Candidates: []Candidate{}, Pages: []PageEvidence{}, Warnings: []string{}, Snapshots: []Document{}}
	fail := func(e error) Result {
		result.Error = AsError(e)
		result.Reason = result.Error.Code
		if (errors.Is(ctx.Err(), context.DeadlineExceeded) || result.Reason == "DURATION_BUDGET") && result.Reason != "OUTCOME_UNCERTAIN" {
			result.Status = "limited"
			result.Reason = "DURATION_BUDGET"
			result.Error = nil
			for _, c := range result.Candidates {
				if !c.Valid {
					result.Status = "partial"
				}
			}
			return result
		}
		if len(result.Candidates) > 0 {
			result.Status = "partial"
		}
		if result.Error.Code == "OUTCOME_UNCERTAIN" {
			result.Status = "awaiting_confirmation"
		}
		return result
	}
	if e := d.Validate(); e != nil {
		return fail(e)
	}
	normalized, e := ValidateInput(d, input)
	if e != nil {
		return fail(e)
	}
	result.Input = normalized
	r, e := Prepare(d, normalized)
	if e != nil {
		return fail(e)
	}
	if mode != "live" && mode != "offline" {
		return fail(Invalid("mode", "需要live/offline"))
	}
	if source == nil {
		return fail(Fault("CAPABILITY_UNAVAILABLE", "fetch", "获取器不可用"))
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(d.Pagination.MaxDurationMS)*time.Millisecond)
	defer cancel()
	defer source.Close()
	result.Checkpoint = Checkpoint{Resume: d.Pagination.Resume, InputHash: Hash(normalized), DefinitionHash: Hash(d)}
	startPage := 1
	resume := options.Checkpoint
	if resume.DefinitionHash != "" && (resume.DefinitionHash != Hash(d) || resume.InputHash != Hash(normalized)) {
		return fail(Fault("CHECKPOINT_CONFLICT", "checkpoint", "检查点不属于当前冻结定义或输入"))
	}
	if resume.Done {
		result.Checkpoint = resume
		result.Status = resume.FinalStatus
		result.Reason = resume.FinalReason
		return result
	}
	continued := resume.Pages > 0 && resume.NextURL != "" && d.Pagination.Resume == "url_or_cursor" && len(d.Actions) == 0 && d.Fetch.Method != "POST"
	if continued {
		result.Checkpoint = resume
		startPage = resume.Pages + 1
		r.URL = resume.NextURL
		parsed, err := url.Parse(r.URL)
		if err != nil {
			return fail(err)
		}
		for k := range parsed.Query() {
			delete(r.HTTP.Query, k)
		}
		if d.Pagination.Kind == "cursor" {
			r.HTTP.Query[d.Pagination.CursorParameter] = resume.Cursor
		}
	}
	pending := []Candidate{}
	total := result.Checkpoint.Records
	flush := func(final bool) error {
		if options.Commit == nil {
			return nil
		}
		cp := result.Checkpoint
		cp.Records = total
		if final {
			cp.Done = true
			cp.FinalStatus = result.Status
			cp.FinalReason = result.Reason
		}
		if e := options.Commit(ctx, pending, cp); e != nil {
			return e
		}
		result.Checkpoint = cp
		pending = nil
		result.Candidates = nil
		return nil
	}
	finish := func() Result {
		if e := flush(true); e != nil {
			return fail(e)
		}
		return result
	}
	doc, e := source.Open(ctx, r)
	if e != nil {
		return fail(e)
	}
	for i, a := range d.Actions {
		if continued {
			break
		}
		if mode == "offline" {
			result.Warnings = append(result.Warnings, "OFFLINE_ACTION_NOT_EXECUTED:"+a.ID)
			continue
		}
		if e = source.Action(ctx, a, r.ActionValues[i]); e != nil {
			return fail(e)
		}
		doc, e = source.Snapshot(ctx)
		if e != nil {
			return fail(e)
		}
	}
	seenPage := map[string]bool{}
	for _, hash := range result.Checkpoint.SeenPages {
		seenPage[hash] = true
	}
	seenIdentity := map[string]string{}
	invalid := 0
	byteCount := 0
	for page := startPage; page <= d.Pagination.MaxPages; page++ {
		if ctx.Err() != nil {
			return fail(Fault("DURATION_BUDGET", "fetch", "已达到时长预算或执行被停止"))
		}
		if e = CheckOrigin(doc.URL, d.Fetch.AllowedOrigins); e != nil {
			return fail(e)
		}
		// load_more can contain previously emitted rows; inspect before deduplicating.
		p, err := Extract(ctx, d, normalized, doc, d.Pagination.MaxRecords+1)
		if err != nil {
			return fail(err)
		}
		if seenPage[p.Hash] {
			return fail(Fault("REPEATED_PAGE", "pagination", "页面记录重复，已停止分页；请核对下一页规则"))
		}
		seenPage[p.Hash] = true
		result.Checkpoint.SeenPages = append(result.Checkpoint.SeenPages, p.Hash)
		result.Pages = append(result.Pages, PageEvidence{URL: doc.URL, ContentHash: Hash(doc.Content), RecordCount: len(p.Candidates), EmptyVerified: p.EmptyVerified})
		result.Checkpoint.Pages = page
		result.Checkpoint.CurrentURL = doc.URL
		result.Checkpoint.ContentHash = Hash(doc.Content)
		result.Checkpoint.EmptyVerified = p.EmptyVerified
		if len(result.Snapshots) < 3 && len(doc.Content) <= 256*1024 {
			result.Snapshots = append(result.Snapshots, doc)
		}
		if len(p.Candidates) == 0 && !p.EmptyVerified {
			return fail(Fault("ZERO_RESULT_UNVERIFIED", "records", "未找到记录且没有空态证据；可能是规则失效、登录页或验证页"))
		}
		for _, c := range p.Candidates {
			if c.Valid && seenIdentity[CandidateKey(c)] == Hash(c.Values) {
				continue
			}
			if total >= d.Pagination.MaxRecords || byteCount+len(JSON(c)) > 1024*1024 {
				result.Status = "limited"
				result.Reason = "RECORD_OR_EVIDENCE_BUDGET"
				if invalid > 0 {
					result.Status = "partial"
				}
				return finish()
			}
			c.Page = page
			if !c.Valid {
				invalid++
			} else if old, ok := seenIdentity[CandidateKey(c)]; ok {
				if old != Hash(c.Values) {
					c.Valid = false
					c.Fields["_identity"] = FieldEvidence{Status: "invalid", Message: "同一身份出现不同结果"}
					invalid++
				} else {
					continue
				}
			}
			if c.Valid {
				seenIdentity[CandidateKey(c)] = Hash(c.Values)
			}
			byteCount += len(JSON(c))
			result.Candidates = append(result.Candidates, c)
			pending = append(pending, c)
			total++
		}
		pconf := d.Pagination
		if p.Truncated {
			result.Status = "limited"
			result.Reason = "RECORD_OR_EVIDENCE_BUDGET"
			if invalid > 0 {
				result.Status = "partial"
			}
			return finish()
		}
		ended := pconf.Kind == "none" || pconf.EndCondition == "empty_verified" && (len(p.Candidates) == 0 || p.PrimaryEmpty) && p.EmptyVerified || pconf.EndCondition == "next_missing" && !p.HasNext
		if ended {
			result.Status = "succeeded"
			if invalid > 0 {
				result.Status = "partial"
				result.Reason = "INVALID_CANDIDATES"
			}
			if mode == "offline" && len(d.Actions) > 0 {
				result.Status = "limited"
				result.Reason = "OFFLINE_ACTIONS_UNCOVERED"
			}
			return finish()
		}
		if !p.HasNext {
			return fail(Fault("END_CONDITION_UNVERIFIED", "pagination", "未找到下一页但约定空态结束尚未确认"))
		}
		if pconf.Kind == "url" || pconf.Kind == "cursor" {
			next := p.Next
			if pconf.Kind == "cursor" {
				u, _ := url.Parse(doc.URL)
				q := u.Query()
				q.Set(pconf.CursorParameter, p.Next)
				u.RawQuery = q.Encode()
				next = u.String()
				result.Checkpoint.Cursor = p.Next
			} else {
				u, _ := url.Parse(doc.URL)
				ref, err := url.Parse(next)
				if err != nil || next == "" {
					return fail(Fault("INVALID_NEXT_URL", "pagination", "下一页元素没有HTTP链接"))
				}
				next = u.ResolveReference(ref).String()
				// A discovered next-page query wins over entry defaults (e.g. start=25).
				parsed, _ := url.Parse(next)
				for k := range parsed.Query() {
					delete(r.HTTP.Query, k)
				}
			}
			if e = CheckOrigin(next, d.Fetch.AllowedOrigins); e != nil {
				return fail(e)
			}
			result.Checkpoint.NextURL = next
			r.URL = next
			if pconf.Kind == "cursor" {
				r.HTTP.Query[pconf.CursorParameter] = p.Next
			}
		}
		if page == pconf.MaxPages || total >= pconf.MaxRecords {
			result.Status = "limited"
			result.Reason = "PAGINATION_BUDGET"
			if invalid > 0 {
				result.Status = "partial"
			}
			return finish()
		}
		if e = flush(false); e != nil {
			return fail(e)
		}
		if options.Commit != nil {
			byteCount = 0
		}
		if pconf.Kind == "click" || pconf.Kind == "load_more" {
			if mode == "offline" {
				result.Status = "limited"
				result.Reason = "OFFLINE_PAGINATION_UNCOVERED"
				return finish()
			}
			a := Action{ID: fmt.Sprintf("page_%d", page), Kind: "click", Locator: pconf.Next, TimeoutMS: 10000, ReplayClass: "receipt_required"}
			if e = source.Action(ctx, a, ""); e == nil {
				doc, e = source.Snapshot(ctx)
			}
		} else {
			doc, e = source.Open(ctx, r)
		}
		if e != nil {
			return fail(e)
		}
	}
	return fail(Fault("PAGINATION_BUDGET", "pagination", "分页预算已耗尽"))
}
