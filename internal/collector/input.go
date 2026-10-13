package collector

import (
	"encoding/json"
	"fmt"
	"github.com/nekoimi/scrapio/internal/capture"
	"github.com/nekoimi/scrapio/internal/editor"
	"github.com/nekoimi/scrapio/internal/field"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var templatePattern = regexp.MustCompile(`\{\{input\.([^{}]+)\}\}`)

func ValidateInput(d Definition, input map[string]any) (map[string]any, error) {
	if input == nil || len(JSON(input)) > 65536 {
		return nil, Invalid("input", "输入必须是64KiB内的JSON对象")
	}
	if Sensitive(input) {
		return nil, Invalid("input", "认证值不能存入样例，请使用credential_ref")
	}
	fields := map[string]Field{}
	for _, f := range d.InputFields {
		fields[f.Key] = f
	}
	for k := range input {
		if _, ok := fields[k]; !ok {
			return nil, Invalid("input."+k, "未知输入字段")
		}
	}
	out := map[string]any{}
	for _, f := range d.InputFields {
		v, present := input[f.Key]
		if !present && len(f.Default) > 0 {
			var e error
			v, e = capture.DecodeJSON(string(f.Default))
			if e != nil {
				return nil, e
			}
			present = true
		}
		if !present {
			if f.Required {
				return nil, Invalid("input."+f.Key, "缺少必填输入")
			}
			continue
		}
		value, e := ValidateValue(v, f, false)
		if e != nil {
			return nil, Invalid("input."+f.Key, e.Error())
		}
		out[f.Key] = value
	}
	return out, nil
}

func Sensitive(v any) bool {
	switch value := v.(type) {
	case map[string]any:
		for k, item := range value {
			if capture.IsSensitiveKey(k) || Sensitive(item) {
				return true
			}
		}
	case []any:
		for _, item := range value {
			if Sensitive(item) {
				return true
			}
		}
	}
	return false
}
func ValidateValue(v any, f Field, convert bool) (any, error) {
	if v == nil {
		if f.Nullable {
			return nil, nil
		}
		return nil, fmt.Errorf("不允许null")
	}
	if f.Multiple {
		a, ok := v.([]any)
		if !ok || len(a) > 100 || f.Required && len(a) == 0 && !f.EmptyAllowed {
			return nil, fmt.Errorf("需要有限数组，必填数组不可空")
		}
		single := f
		single.Multiple = false
		single.Nullable = false
		out := make([]any, 0, len(a))
		for _, x := range a {
			y, e := ValidateValue(x, single, convert)
			if e != nil {
				return nil, e
			}
			out = append(out, y)
		}
		return out, nil
	}
	if convert && f.Type != "json" && f.Type != "url" {
		s, ok := v.(string)
		if ok && s != "" {
			r, code := field.Convert(s, field.Options{Type: f.Type})
			if code != "" {
				return nil, fmt.Errorf("类型转换失败：%s", code)
			}
			v = r
		}
	}
	switch f.Type {
	case "string", "url", "date", "datetime":
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("需要%s字符串", f.Type)
		}
		if s == "" && !f.EmptyAllowed {
			return nil, fmt.Errorf("不允许空字符串")
		}
		switch f.Type {
		case "url":
			if _, e := capture.ValidateURL(s); e != nil {
				return nil, e
			}
		case "date":
			if _, e := time.Parse("2006-01-02", s); e != nil {
				return nil, fmt.Errorf("需要ISO日期")
			}
		case "datetime":
			if _, e := time.Parse(time.RFC3339, s); e != nil {
				return nil, fmt.Errorf("需要带时区的ISO时间")
			}
		}
	case "integer", "number":
		var n float64
		switch x := v.(type) {
		case json.Number:
			var e error
			n, e = x.Float64()
			if e != nil {
				return nil, fmt.Errorf("无效数字")
			}
		case float64:
			n = x
		case int:
			n = float64(x)
		default:
			return nil, fmt.Errorf("需要JSON数字")
		}
		if math.IsNaN(n) || math.IsInf(n, 0) || f.Type == "integer" && (math.Trunc(n) != n || math.Abs(n) > 9007199254740991) {
			return nil, fmt.Errorf("需要有限数字/安全整数，标识请用string")
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return nil, fmt.Errorf("需要JSON布尔值")
		}
	case "json":
		if len(JSON(v)) > 65536 {
			return nil, fmt.Errorf("JSON值过大")
		}
		if _, err := capture.DecodeJSON(JSON(v)); err != nil {
			return nil, fmt.Errorf("JSON嵌套或值预算超限")
		}
	default:
		return nil, fmt.Errorf("未知字段类型")
	}
	return v, nil
}
func Scalar(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case json.Number:
		return x.String(), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	default:
		return "", fmt.Errorf("绑定需要非null标量")
	}
}
func Resolve(b Binding, input map[string]any) (any, error) {
	switch b.Kind {
	case "field":
		v, ok := input[strings.TrimPrefix(b.Path, "input.")]
		if !ok {
			return nil, Invalid(b.Path, "绑定字段缺失")
		}
		return v, nil
	case "constant":
		return capture.DecodeJSON(string(b.Value))
	case "template":
		var s string
		if json.Unmarshal(b.Value, &s) != nil {
			return nil, Invalid("template", "模板必须为字符串")
		}
		var e error
		s = templatePattern.ReplaceAllStringFunc(s, func(raw string) string {
			k := templatePattern.FindStringSubmatch(raw)[1]
			v, ok := input[k]
			if !ok {
				e = Invalid("input."+k, "模板字段缺失")
				return ""
			}
			r, err := Scalar(v)
			if err != nil {
				e = Invalid("input."+k, err.Error())
			}
			return r
		})
		return s, e
	}
	return nil, Invalid("binding", "无效绑定")
}

type Request struct {
	URL          string
	HTTP         capture.HTTPRequest
	ActionValues []string
}

func Prepare(d Definition, input map[string]any) (Request, error) {
	r := Request{HTTP: capture.HTTPRequest{Method: d.Fetch.Method, TimeoutMS: 10000, CredentialRef: d.Fetch.CredentialRef, Query: map[string]string{}}}
	v, e := Resolve(d.Fetch.URL, input)
	if e != nil {
		return r, e
	}
	r.URL, e = Scalar(v)
	if e != nil {
		return r, Invalid("fetch.url", e.Error())
	}
	if e = CheckOrigin(r.URL, d.Fetch.AllowedOrigins); e != nil {
		return r, e
	}
	for k, b := range d.Fetch.Query {
		v, e = Resolve(b, input)
		if e != nil {
			return r, e
		}
		r.HTTP.Query[k], e = Scalar(v)
		if e != nil {
			return r, Invalid("fetch.query."+k, e.Error())
		}
	}
	if d.Fetch.Body != nil {
		v, e = Resolve(*d.Fetch.Body, input)
		if e != nil {
			return r, e
		}
		r.HTTP.Body = json.RawMessage(JSON(v))
	}
	if e = r.HTTP.Validate(); e != nil {
		return r, Invalid("fetch", e.Error())
	}
	for _, a := range d.Actions {
		value := ""
		if a.Value != nil {
			v, e = Resolve(*a.Value, input)
			if e != nil {
				return r, e
			}
			value, e = Scalar(v)
			if e != nil {
				return r, e
			}
		}
		if a.Kind == "navigate" {
			if e = CheckOrigin(value, d.Fetch.AllowedOrigins); e != nil {
				return r, e
			}
		}
		command := editor.Command{PageStateID: "preflight", ExpectedRevision: 1, Type: a.Kind, Value: value, TimeoutMS: a.TimeoutMS, Confirmed: true}
		if a.Locator != nil {
			command.Locator = &editor.Locator{Strategy: a.Locator.Strategy, Expression: a.Locator.Expression}
		}
		if e = command.Validate(); e != nil {
			return r, Invalid("actions."+a.ID, e.Error())
		}
		r.ActionValues = append(r.ActionValues, value)
	}
	return r, nil
}
func CheckOrigin(raw string, origins []string) error {
	u, e := capture.ValidateURL(raw)
	if e != nil {
		return Invalid("fetch.url", e.Error())
	}
	for _, o := range origins {
		if u.Scheme+"://"+u.Host == o {
			return nil
		}
	}
	return &Error{Code: "ORIGIN_OUT_OF_SCOPE", Message: "目标地址不在此采集器允许的来源中，请在获取设置中核对实际来源", Location: map[string]string{"step_id": "fetch"}, Details: map[string]any{"actual_origin": u.Scheme + "://" + u.Host, "allowed_origins": origins}}
}
