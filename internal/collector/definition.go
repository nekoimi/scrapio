// Package collector implements collector.v1 without the legacy list/detail DSL.
package collector

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/nekoimi/scrapio/internal/capture"
)

const Interpreter = "co.interpreter.v1"
const MaxDefinition = 256 * 1024

type Error struct {
	Code      string            `json:"code"`
	Message   string            `json:"message"`
	Retryable bool              `json:"retryable"`
	Location  map[string]string `json:"location,omitempty"`
	Details   map[string]any    `json:"details,omitempty"`
}

func (e *Error) Error() string { return e.Message }
func Invalid(path, message string) error {
	code := "INVALID_CONFIGURATION"
	if strings.HasPrefix(path, "input.") {
		code = "INPUT_FIELD_INVALID"
	}
	return &Error{Code: code, Message: message, Location: map[string]string{"field": path}}
}
func Fault(code, path, message string) error {
	return &Error{Code: code, Message: message, Location: map[string]string{"step_id": path}}
}

type Field struct {
	Key          string          `json:"key"`
	Label        string          `json:"label,omitempty"`
	Type         string          `json:"type"`
	Required     bool            `json:"required"`
	Nullable     bool            `json:"nullable"`
	Multiple     bool            `json:"multiple"`
	EmptyAllowed bool            `json:"empty_allowed,omitempty"`
	Default      json.RawMessage `json:"default,omitempty"`
}
type Binding struct {
	Kind  string          `json:"kind"`
	Path  string          `json:"path,omitempty"`
	Value json.RawMessage `json:"value,omitempty"`
}
type Locator struct {
	Strategy   string `json:"strategy"`
	Expression string `json:"expression"`
}
type Fetch struct {
	Kind           string             `json:"kind"`
	URL            Binding            `json:"url"`
	Method         string             `json:"method,omitempty"`
	Query          map[string]Binding `json:"query,omitempty"`
	Body           *Binding           `json:"body,omitempty"`
	CredentialRef  string             `json:"credential_ref,omitempty"`
	AllowedOrigins []string           `json:"allowed_origins"`
}
type Action struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Locator     *Locator `json:"locator,omitempty"`
	Value       *Binding `json:"value,omitempty"`
	TimeoutMS   int      `json:"timeout_ms"`
	ReplayClass string   `json:"replay_class"`
}
type Mapping struct {
	Key       string          `json:"key"`
	Source    string          `json:"source"`
	InputKey  string          `json:"input_key,omitempty"`
	Value     json.RawMessage `json:"value,omitempty"`
	Locator   *Locator        `json:"locator,omitempty"`
	Extract   string          `json:"extract,omitempty"`
	Attribute string          `json:"attribute,omitempty"`
	Clean     string          `json:"clean,omitempty"`
}
type Output struct {
	Port           string    `json:"port"`
	Fields         []Field   `json:"fields"`
	IdentityFields []string  `json:"identity_fields"`
	Mapping        []Mapping `json:"mapping"`
}
type Pagination struct {
	Kind            string   `json:"kind"`
	Next            *Locator `json:"next,omitempty"`
	CursorParameter string   `json:"cursor_parameter,omitempty"`
	EmptyMarker     *Locator `json:"empty_marker,omitempty"`
	EndCondition    string   `json:"end_condition"`
	Resume          string   `json:"resume"`
	MaxPages        int      `json:"max_pages"`
	MaxRecords      int      `json:"max_records"`
	MaxDurationMS   int      `json:"max_duration_ms"`
}
type Definition struct {
	DefinitionVersion  string     `json:"definition_version"`
	InterpreterVersion string     `json:"interpreter_version"`
	InputFields        []Field    `json:"input_fields"`
	Fetch              Fetch      `json:"fetch"`
	Actions            []Action   `json:"actions"`
	Records            Locator    `json:"records"`
	Output             Output     `json:"output"`
	Pagination         Pagination `json:"pagination"`
}

func Decode(raw []byte) (Definition, error) {
	var d Definition
	if len(raw) > MaxDefinition {
		return d, Invalid("definition", "定义超过256KiB")
	}
	if err := Strict(raw, &d); err != nil {
		return d, Invalid("definition", err.Error())
	}
	return d, d.Validate()
}
func Strict(raw []byte, target any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("只能提交一个JSON对象")
	}
	return nil
}
func JSON(value any) string { data, _ := json.Marshal(value); return string(data) }
func Hash(value any) string { return capture.Hash([]byte(JSON(value))) }
func OutputSchemaHash(d Definition) string {
	return Hash(struct {
		Port           string   `json:"port"`
		Fields         []Field  `json:"fields"`
		IdentityFields []string `json:"identity_fields"`
	}{d.Output.Port, d.Output.Fields, d.Output.IdentityFields})
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ValidateSchema shares field and stable-identity semantics with logical tables.
func ValidateSchema(fields []Field, identity []string) error {
	keys, e := validateFields(fields, "schema.fields")
	if e != nil {
		return e
	}
	if len(identity) < 1 || len(identity) > 5 {
		return Invalid("schema.unique_key", "需要1..5个唯一键字段")
	}
	seen := map[string]bool{}
	for _, key := range identity {
		f, ok := keys[key]
		if !ok || seen[key] || f.Multiple || f.Nullable || !f.Required || f.Type == "json" {
			return Invalid("schema.unique_key", "唯一键必须是非空必填标量")
		}
		seen[key] = true
	}
	return nil
}

func validateFields(fields []Field, path string) (map[string]Field, error) {
	if len(fields) < 1 || len(fields) > 30 {
		return nil, Invalid(path, "需要1..30个字段")
	}
	keys := map[string]Field{}
	for _, f := range fields {
		if !keyPattern.MatchString(f.Key) || capture.IsSensitiveKey(f.Key) || len(f.Label) > 64 {
			return nil, Invalid(path+"."+f.Key, "字段键或显示名无效；凭据请使用引用")
		}
		if _, ok := keys[f.Key]; ok {
			return nil, Invalid(path+"."+f.Key, "字段键重复")
		}
		switch f.Type {
		case "string", "url", "integer", "number", "boolean", "date", "datetime", "json":
		default:
			return nil, Invalid(path+"."+f.Key, "不支持此字段类型")
		}
		keys[f.Key] = f
		if len(f.Default) > 0 {
			v, err := capture.DecodeJSON(string(f.Default))
			if err != nil {
				return nil, Invalid(path+"."+f.Key, "默认值JSON无效或超过嵌套预算")
			}
			if Sensitive(v) {
				return nil, Invalid(path+"."+f.Key, "默认值含认证字段，请使用凭据引用")
			}
			if _, err = ValidateValue(v, f, false); err != nil {
				return nil, Invalid(path+"."+f.Key, "默认值不符合字段类型")
			}
		}
	}
	return keys, nil
}
func (d Definition) Validate() error {
	if d.DefinitionVersion != "collector.v1" || d.InterpreterVersion != Interpreter {
		return Invalid("definition_version", "新采集器只接受collector.v1 / co.interpreter.v1")
	}
	inputs, err := validateFields(d.InputFields, "input_fields")
	if err != nil {
		return err
	}
	outputs, err := validateFields(d.Output.Fields, "output.fields")
	if err != nil {
		return err
	}
	if d.Fetch.Kind != "http_html" && d.Fetch.Kind != "http_json" && d.Fetch.Kind != "browser" {
		return Invalid("fetch.kind", "请选择HTTP HTML、HTTP JSON或浏览器")
	}
	if len(d.Fetch.AllowedOrigins) < 1 || len(d.Fetch.AllowedOrigins) > 10 {
		return Invalid("fetch.allowed_origins", "需要1..10个允许的来源")
	}
	for _, origin := range d.Fetch.AllowedOrigins {
		u, e := capture.ValidateURL(origin)
		if e != nil || origin != u.Scheme+"://"+u.Host {
			return Invalid("fetch.allowed_origins", "来源必须为scheme://host[:port]，不能包含路径")
		}
	}
	if err = validateBinding(d.Fetch.URL, inputs, false); err != nil {
		return Invalid("fetch.url", err.Error())
	}
	if d.Fetch.Method != "" && d.Fetch.Method != "GET" && d.Fetch.Method != "POST" {
		return Invalid("fetch.method", "只支持GET/POST")
	}
	if d.Fetch.Kind == "browser" && (d.Fetch.Method == "POST" || len(d.Fetch.Query) > 0 || d.Fetch.Body != nil) {
		return Invalid("fetch", "浏览器入口只接受URL；HTTP请求参数请使用HTTP获取")
	}
	if len(d.Fetch.Query) > 30 {
		return Invalid("fetch.query", "最多30个查询参数")
	}
	for k, b := range d.Fetch.Query {
		if len(k) > 128 || capture.IsSensitiveKey(k) {
			return Invalid("fetch.query."+k, "参数键无效，认证请使用credential_ref")
		}
		if err = validateBinding(b, inputs, false); err != nil {
			return Invalid("fetch.query."+k, err.Error())
		}
	}
	if d.Fetch.Body != nil {
		if d.Fetch.Method != "POST" {
			return Invalid("fetch.body", "请求体需要POST")
		}
		if err = validateBinding(*d.Fetch.Body, inputs, true); err != nil {
			return err
		}
	}
	req := capture.HTTPRequest{Method: d.Fetch.Method, CredentialRef: d.Fetch.CredentialRef}
	if err = req.Validate(); err != nil {
		return Invalid("fetch", err.Error())
	}
	if d.Fetch.Kind == "browser" && strings.HasPrefix(d.Fetch.CredentialRef, "${secret:") {
		return Invalid("fetch.credential_ref", "浏览器使用${credential:UUID}受控Cookie凭据；secret引用仅用于HTTP")
	}
	if len(d.Actions) > 50 {
		return Invalid("actions", "最多50个动作")
	}
	ids := map[string]bool{}
	for _, a := range d.Actions {
		if d.Fetch.Kind != "browser" {
			return Invalid("actions", "网页动作只用于浏览器采集器")
		}
		if !keyPattern.MatchString(a.ID) || ids[a.ID] || a.TimeoutMS < 100 || a.TimeoutMS > 30000 {
			return Invalid("actions."+a.ID, "需要唯一动作ID和100..30000ms超时")
		}
		ids[a.ID] = true
		switch a.Kind {
		case "navigate", "click", "input", "wait", "scroll":
		default:
			return Invalid("actions."+a.ID, "不支持的动作")
		}
		if a.ReplayClass != "read_only" && a.ReplayClass != "receipt_required" || ((a.Kind == "click" || a.Kind == "input") && a.ReplayClass != "receipt_required") {
			return Invalid("actions."+a.ID, "点击/输入必须查询回执，不能声明自动重放")
		}
		if a.Locator != nil {
			if err = a.Locator.Validate(false); err != nil {
				return Invalid("actions."+a.ID+".locator", "动作定位器无效："+err.Error())
			}
		}
		if (a.Kind == "click" || a.Kind == "input") && a.Locator == nil {
			return Invalid("actions."+a.ID, "需要定位器")
		}
		if a.Value != nil {
			if err = validateBinding(*a.Value, inputs, false); err != nil {
				return err
			}
		} else if a.Kind != "click" && !(a.Kind == "wait" && a.Locator != nil) {
			return Invalid("actions."+a.ID, "需要动作值绑定")
		}
	}
	if err = d.Records.Validate(d.Fetch.Kind == "http_json"); err != nil {
		return Invalid("records", err.Error())
	}
	if d.Output.Port != "records" || len(d.Output.IdentityFields) < 1 || len(d.Output.IdentityFields) > 5 || len(d.Output.Mapping) != len(outputs) {
		return Invalid("output", "需要records端口、1..5个稳定身份字段及每字段一个映射")
	}
	seen := map[string]bool{}
	for _, k := range d.Output.IdentityFields {
		f, ok := outputs[k]
		if !ok || seen[k] || f.Multiple || f.Nullable || !f.Required || f.Type == "json" {
			return Invalid("output.identity_fields", "身份字段必须是非空必填单值标量（业务ID建议string）")
		}
		seen[k] = true
	}
	seen = map[string]bool{}
	for _, m := range d.Output.Mapping {
		if _, ok := outputs[m.Key]; !ok || seen[m.Key] {
			return Invalid("output.mapping."+m.Key, "映射字段缺失或重复")
		}
		seen[m.Key] = true
		switch m.Source {
		case "input":
			if _, ok := inputs[m.InputKey]; !ok {
				return Invalid("output.mapping."+m.Key, "输入透传字段不存在")
			}
		case "constant":
			if len(m.Value) == 0 {
				return Invalid("output.mapping."+m.Key, "需要显式常量")
			}
			v, e := capture.DecodeJSON(string(m.Value))
			if e != nil {
				return Invalid("output.mapping."+m.Key, "常量JSON无效或超过嵌套预算")
			}
			if Sensitive(v) {
				return Invalid("output.mapping."+m.Key, "常量含认证字段，请使用凭据引用")
			}
			if _, e = ValidateValue(v, outputs[m.Key], false); e != nil {
				return Invalid("output.mapping."+m.Key, e.Error())
			}
		case "element":
			if m.Locator == nil {
				return Invalid("output.mapping."+m.Key, "需要当前记录定位器")
			}
			if err = m.Locator.Validate(d.Fetch.Kind == "http_json"); err != nil {
				return Invalid("output.mapping."+m.Key+".locator", "字段定位器无效："+err.Error())
			}
			if m.Extract != "text" && m.Extract != "link" && m.Extract != "image" && m.Extract != "attribute" {
				return Invalid("output.mapping."+m.Key, "请选择提取方式")
			}
			if m.Extract == "attribute" && (m.Attribute == "" || len(m.Attribute) > 128) {
				return Invalid("output.mapping."+m.Key, "需要属性名")
			}
		default:
			return Invalid("output.mapping."+m.Key, "只支持element/input/constant")
		}
		if m.Clean != "" && m.Clean != "none" && m.Clean != "trim" && m.Clean != "whitespace" {
			return Invalid("output.mapping."+m.Key, "不支持的清洗方式")
		}
	}
	p := d.Pagination
	if p.MaxPages < 1 || p.MaxPages > 100 || p.MaxRecords < 1 || p.MaxRecords > 10000 || p.MaxDurationMS < 1 || p.MaxDurationMS > 600000 {
		return Invalid("pagination", "需要有效页数、记录数和时长预算")
	}
	if p.EndCondition != "single_page" && p.EndCondition != "next_missing" && p.EndCondition != "empty_verified" {
		return Invalid("pagination.end_condition", "需要明确结束条件")
	}
	if p.Kind == "none" {
		if p.EndCondition != "single_page" || p.MaxPages != 1 {
			return Invalid("pagination", "单页模式需要single_page、max_pages=1")
		}
	} else {
		switch p.Kind {
		case "url", "cursor", "click", "load_more":
		default:
			return Invalid("pagination.kind", "不支持的分页")
		}
		if p.EndCondition == "single_page" || p.Next == nil {
			return Invalid("pagination", "分页需要next定位器及结束依据")
		}
		if err = p.Next.Validate(d.Fetch.Kind == "http_json"); err != nil {
			return Invalid("pagination.next", "分页定位器无效："+err.Error())
		}
		if (p.Kind == "click" || p.Kind == "load_more") && (d.Fetch.Kind != "browser" || p.Resume != "restart_item") {
			return Invalid("pagination", "点击分页需要浏览器并声明restart_item")
		}
		if p.Kind == "cursor" && (d.Fetch.Kind != "http_json" || !keyPattern.MatchString(p.CursorParameter)) {
			return Invalid("pagination.cursor_parameter", "游标分页需要HTTP JSON及明确查询参数名")
		}
	}
	if p.Resume != "url_or_cursor" && p.Resume != "restart_item" {
		return Invalid("pagination.resume", "只支持URL/游标检查点或从头重采")
	}
	if p.EndCondition == "empty_verified" && p.EmptyMarker == nil {
		return Invalid("pagination.empty_marker", "空结果成功需要显式空态标识定位器")
	}
	if p.EmptyMarker != nil {
		if err = p.EmptyMarker.Validate(d.Fetch.Kind == "http_json"); err != nil {
			return Invalid("pagination.empty_marker", "空态定位器无效："+err.Error())
		}
	}
	return nil
}
func validateBinding(b Binding, inputs map[string]Field, allowJSON bool) error {
	switch b.Kind {
	case "field":
		if !strings.HasPrefix(b.Path, "input.") {
			return fmt.Errorf("采集器绑定必须为input.字段")
		}
		f, ok := inputs[strings.TrimPrefix(b.Path, "input.")]
		if !ok || (!allowJSON && (f.Type == "json" || f.Multiple)) {
			return fmt.Errorf("未知输入字段或非标量绑定")
		}
	case "constant":
		if len(b.Value) == 0 || len(b.Value) > 65536 || !json.Valid(b.Value) {
			return fmt.Errorf("常量必须是有限JSON值")
		}
		v, e := capture.DecodeJSON(string(b.Value))
		if e != nil || Sensitive(v) {
			return fmt.Errorf("常量含有认证字段，请使用凭据引用")
		}
		if !allowJSON {
			var v any
			_ = json.Unmarshal(b.Value, &v)
			if _, e := Scalar(v); e != nil {
				return e
			}
		}
	case "template":
		var s string
		if json.Unmarshal(b.Value, &s) != nil || len(s) > 2048 {
			return fmt.Errorf("模板必须为2048字符内的字符串")
		}
		for _, m := range templatePattern.FindAllStringSubmatch(s, -1) {
			if !keyPattern.MatchString(m[1]) {
				return fmt.Errorf("模板仅支持{{input.字段}}")
			}
			f, ok := inputs[m[1]]
			if !ok || f.Type == "json" || f.Multiple {
				return fmt.Errorf("模板变量必须是已声明标量输入")
			}
		}
		if strings.Contains(templatePattern.ReplaceAllString(s, ""), "{{") || strings.Contains(templatePattern.ReplaceAllString(s, ""), "}}") {
			return fmt.Errorf("模板语法无效")
		}
	default:
		return fmt.Errorf("只支持field/constant/template绑定")
	}
	return nil
}
