package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
	"github.com/dop251/goja/ast"
	"github.com/dop251/goja/parser"
	"github.com/nekoimi/scrapio/internal/capture"
)

const TransformVersion = "co.transform.v1"
const TransformBytes = 8192
const TransformTimeout = 25 * time.Millisecond

// Transform is a pure expression on one typed value. Target Schema belongs to
// the collector output or the receiving edge input, never to the expression.
type Transform struct {
	Version    string            `json:"version"`
	Input      Field             `json:"input"`
	Expression string            `json:"expression"`
	Samples    []TransformSample `json:"samples"`
}
type TransformSample struct {
	Value    json.RawMessage `json:"value"`
	Expected json.RawMessage `json:"expected"`
}
type TransformEvidence struct {
	Version string `json:"version"`
	Hash    string `json:"hash"`
	Before  any    `json:"before"`
	After   any    `json:"after"`
	Error   *Error `json:"error,omitempty"`
}

func PreviewTransform(ctx context.Context, t *Transform, value any, target Field) (any, error) {
	if t == nil {
		return nil, Invalid("transform", "需要转换配置")
	}
	if _, e := validateFields([]Field{target}, "target"); e != nil {
		return nil, e
	}
	if e := t.Check(target, "transform"); e != nil {
		return nil, e
	}
	output, evidence, e := t.Apply(ctx, value, target, false, "transform")
	if e != nil {
		return map[string]any{"evidence": evidence, "formal_writes": 0}, e
	}
	return map[string]any{"value": output, "evidence": evidence, "target": target, "formal_writes": 0}, nil
}

func transformFault(code, path, message string, t *Transform) error {
	return &Error{Code: code, Message: message, Location: map[string]string{"field": path}, Details: map[string]any{"transform_hash": Hash(t), "transform_version": TransformVersion}}
}
func (t *Transform) validate(path string) (*goja.Program, error) {
	if t == nil {
		return nil, nil
	}
	if t.Version != TransformVersion || t.Input.Key != "value" || len(t.Input.Default) > 0 || len(t.Samples) < 1 || len(t.Samples) > 5 {
		return nil, Invalid(path, "转换需要co.transform.v1、value输入Schema（无默认值）及1..5个离线输入/预期样例")
	}
	if _, e := validateFields([]Field{t.Input}, path+".input"); e != nil {
		return nil, e
	}
	p, e := compileTransform(t.Expression)
	if e != nil {
		return nil, transformFault("TRANSFORM_INVALID", path, e.Error(), t)
	}
	for _, sample := range t.Samples {
		if len(sample.Value) == 0 || len(sample.Expected) == 0 || len(sample.Value) > TransformBytes || len(sample.Expected) > TransformBytes {
			return nil, Invalid(path+".samples", "样例输入及预期各需1..8192字节JSON")
		}
		var v, expected any
		if e := Strict(sample.Value, &v); e != nil {
			return nil, Invalid(path+".samples", e.Error())
		}
		if e := Strict(sample.Expected, &expected); e != nil {
			return nil, Invalid(path+".samples", e.Error())
		}
		if Sensitive(v) || Sensitive(expected) {
			return nil, Invalid(path+".samples", "样例不可包含认证值")
		}
	}
	return p, nil
}
func (t *Transform) Check(target Field, path string) error {
	if t == nil {
		return nil
	}
	program, e := t.validate(path)
	if e != nil {
		return e
	}
	for i, sample := range t.Samples {
		var value any
		_ = Strict(sample.Value, &value)
		actual, _, e := t.run(context.Background(), program, value, target, false, path)
		if e != nil {
			return e
		}
		var expected any
		_ = Strict(sample.Expected, &expected)
		if Hash(actual) != Hash(expected) {
			return transformFault("TRANSFORM_SAMPLE_MISMATCH", fmt.Sprintf("%s.samples.%d", path, i), "转换样例实际值与预期不一致，请修正表达式或明确更新预期", t)
		}
	}
	return nil
}
func (t *Transform) Apply(ctx context.Context, value any, target Field, convert bool, path string) (any, *TransformEvidence, error) {
	if t == nil {
		return value, nil, nil
	}
	p, e := t.validate(path)
	if e != nil {
		return nil, nil, e
	}
	return t.run(ctx, p, value, target, convert, path)
}
func (t *Transform) run(ctx context.Context, p *goja.Program, value any, target Field, convert bool, path string) (any, *TransformEvidence, error) {
	evidence := &TransformEvidence{Version: t.Version, Hash: Hash(t), Before: value}
	fail := func(code, message string) (any, *TransformEvidence, error) {
		e := transformFault(code, path, message, t)
		evidence.Error = AsError(e)
		return nil, evidence, e
	}
	// Every intermediate expression is checked, so concatenation or join cannot
	// grow unchecked through nested calls. Native operations have bounded inputs.
	if e := boundedTransformValue(value); e != nil {
		evidence.Before = map[string]any{"omitted": true}
		return fail("TRANSFORM_INPUT_INVALID", e.Error())
	}
	input, e := ValidateValue(value, t.Input, convert)
	if e != nil {
		return fail("TRANSFORM_INPUT_INVALID", e.Error())
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if e = ctx.Err(); e != nil {
		return fail("TRANSFORM_CANCELLED", "转换已取消")
	}
	vm := goja.New()
	vm.SetMaxCallStackSize(64)
	inputJSON := JSON(input)
	v, e := vm.RunString("JSON.parse(" + strconv.Quote(inputJSON) + ")")
	if e != nil {
		return fail("TRANSFORM_INPUT_INVALID", "输入JSON无法载入")
	}
	_ = vm.Set("value", v)
	_ = vm.Set("__bounded", func(call goja.FunctionCall) goja.Value {
		v := call.Argument(0)
		if goja.IsUndefined(v) {
			panic(vm.NewTypeError("转换结果不能为undefined"))
		}
		if e := boundedTransformValue(v.Export()); e != nil {
			panic(vm.NewTypeError(e.Error()))
		}
		return v
	})
	deadline := time.AfterFunc(TransformTimeout, func() { vm.Interrupt("transform timeout") })
	defer deadline.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("transform cancelled")
		case <-done:
		}
	}()
	result, e := vm.RunProgram(p)
	if e != nil {
		if ctx.Err() != nil {
			return fail("TRANSFORM_CANCELLED", "转换已取消")
		}
		if _, ok := e.(*goja.InterruptedError); ok {
			return fail("TRANSFORM_TIMEOUT", "转换超过25ms预算")
		}
		// Do not echo user strings embedded in engine exceptions.
		return fail("TRANSFORM_EXECUTION_FAILED", "表达式执行失败或中间值超出预算；请使用离线样例检查类型及大小")
	}
	raw := result.Export()
	if e = boundedTransformValue(raw); e != nil {
		return fail("TRANSFORM_RESULT_INVALID", e.Error())
	}
	// JSON-normalize to the same types as frozen documents/outbox, preserving null.
	normalized, e := capture.DecodeJSON(JSON(raw))
	if e != nil {
		return fail("TRANSFORM_RESULT_INVALID", e.Error())
	}
	output, e := ValidateValue(normalized, target, false)
	if e != nil {
		return fail("TRANSFORM_RESULT_INVALID", e.Error())
	}
	evidence.After = output
	return output, evidence, nil
}
func boundedTransformValue(value any) error {
	var walk func(any, int) error
	walk = func(v any, depth int) error {
		if depth > 16 {
			return fmt.Errorf("转换JSON嵌套最多16层")
		}
		switch x := v.(type) {
		case []any:
			if len(x) > 100 {
				return fmt.Errorf("转换数组最多100项")
			}
			for _, v := range x {
				if e := walk(v, depth+1); e != nil {
					return e
				}
			}
		case map[string]any:
			if len(x) > 30 {
				return fmt.Errorf("转换对象最多30个键")
			}
			for k, v := range x {
				if forbiddenTransformMember(k) {
					return fmt.Errorf("转换对象包含保留键")
				}
				if e := walk(v, depth+1); e != nil {
					return e
				}
			}
		}
		return nil
	}
	if e := walk(value, 0); e != nil {
		return e
	}
	raw, e := json.Marshal(value)
	if e != nil || len(raw) > TransformBytes {
		return fmt.Errorf("转换值须为8192字节内的有限JSON")
	}
	if Sensitive(value) {
		return fmt.Errorf("转换值含认证字段，请使用凭据引用")
	}
	return nil
}
func forbiddenTransformMember(name string) bool {
	switch name {
	case "constructor", "prototype", "__proto__", "__defineGetter__", "__defineSetter__", "__lookupGetter__", "__lookupSetter__":
		return true
	}
	return false
}

// Compile only an allowlisted JS expression. Emit a guard at every evaluated
// node. Never execute the user's original source as a statement/function body.
func compileTransform(source string) (*goja.Program, error) {
	if strings.TrimSpace(source) == "" || len(source) > TransformBytes {
		return nil, fmt.Errorf("JS表达式需要1..8192字节")
	}
	tree, e := parser.ParseFile(nil, "transform.js", "("+source+"\n)", 0)
	if e != nil || len(tree.Body) != 1 {
		return nil, fmt.Errorf("需要一个JS表达式，不支持语句或函数")
	}
	statement, ok := tree.Body[0].(*ast.ExpressionStatement)
	if !ok {
		return nil, fmt.Errorf("只接受JS表达式")
	}
	count := 0
	var emit func(ast.Expression, int) (string, error)
	emit = func(node ast.Expression, depth int) (string, error) {
		count++
		if count > 256 || depth > 24 {
			return "", fmt.Errorf("表达式最多256节点、24层")
		}
		sub := func(n ast.Expression) (string, error) { return emit(n, depth+1) }
		var text string
		switch n := node.(type) {
		case *ast.Identifier:
			if n.Name.String() != "value" {
				return "", fmt.Errorf("只能读取value；函数需使用白名单")
			}
			text = "value"
		case *ast.StringLiteral:
			text = strconv.Quote(n.Value.String())
		case *ast.NumberLiteral:
			if strings.HasSuffix(n.Literal, "n") {
				return "", fmt.Errorf("不支持BigInt")
			}
			text = n.Literal
		case *ast.BooleanLiteral:
			if n.Value {
				text = "true"
			} else {
				text = "false"
			}
		case *ast.NullLiteral:
			text = "null"
		case *ast.BinaryExpression:
			op := n.Operator.String()
			switch op {
			case "+", "-", "*", "/", "%", "**", "==", "!=", "===", "!==", "<", "<=", ">", ">=", "&&", "||", "??":
			default:
				return "", fmt.Errorf("不支持此运算符")
			}
			left, e := sub(n.Left)
			if e != nil {
				return "", e
			}
			right, e := sub(n.Right)
			if e != nil {
				return "", e
			}
			text = "(" + left + op + right + ")"
		case *ast.UnaryExpression:
			op := n.Operator.String()
			if n.Postfix || (op != "!" && op != "+" && op != "-") {
				return "", fmt.Errorf("不支持赋值或此一元运算")
			}
			v, e := sub(n.Operand)
			if e != nil {
				return "", e
			}
			text = "(" + op + v + ")"
		case *ast.ConditionalExpression:
			test, e := sub(n.Test)
			if e != nil {
				return "", e
			}
			yes, e := sub(n.Consequent)
			if e != nil {
				return "", e
			}
			no, e := sub(n.Alternate)
			if e != nil {
				return "", e
			}
			text = "(" + test + "?" + yes + ":" + no + ")"
		case *ast.DotExpression:
			name := n.Identifier.Name.String()
			if forbiddenTransformMember(name) {
				return "", fmt.Errorf("禁止访问原型或构造器")
			}
			left, e := sub(n.Left)
			if e != nil {
				return "", e
			}
			text = "(" + left + ")." + name
		case *ast.BracketExpression:
			var key string
			switch m := n.Member.(type) {
			case *ast.StringLiteral:
				if forbiddenTransformMember(m.Value.String()) {
					return "", fmt.Errorf("禁止访问原型或构造器")
				}
				key = strconv.Quote(m.Value.String())
			case *ast.NumberLiteral:
				key = m.Literal
			default:
				return "", fmt.Errorf("索引仅支持固定字符串或数字")
			}
			left, e := sub(n.Left)
			if e != nil {
				return "", e
			}
			text = "(" + left + ")[" + key + "]"
		case *ast.ArrayLiteral:
			if len(n.Value) > 100 {
				return "", fmt.Errorf("数组字面量最多100项")
			}
			parts := []string{}
			for _, v := range n.Value {
				p, e := sub(v)
				if e != nil {
					return "", e
				}
				parts = append(parts, p)
			}
			text = "[" + strings.Join(parts, ",") + "]"
		case *ast.ObjectLiteral:
			if len(n.Value) > 30 {
				return "", fmt.Errorf("对象最多30个键")
			}
			parts := []string{}
			for _, v := range n.Value {
				p, ok := v.(*ast.PropertyKeyed)
				if !ok || p.Computed || p.Kind != ast.PropertyKindValue {
					return "", fmt.Errorf("对象仅支持固定键和值，无方法、展开或访问器")
				}
				var key string
				switch k := p.Key.(type) {
				case *ast.Identifier:
					key = k.Name.String()
				case *ast.StringLiteral:
					key = k.Value.String()
				default:
					return "", fmt.Errorf("对象键仅支持字符串")
				}
				if forbiddenTransformMember(key) {
					return "", fmt.Errorf("禁止保留键")
				}
				value, e := sub(p.Value)
				if e != nil {
					return "", e
				}
				parts = append(parts, strconv.Quote(key)+":"+value)
			}
			text = "({" + strings.Join(parts, ",") + "})"
		case *ast.CallExpression:
			if len(n.ArgumentList) > 5 {
				return "", fmt.Errorf("函数最多5个参数")
			}
			var callee string
			switch c := n.Callee.(type) {
			case *ast.Identifier:
				name := c.Name.String()
				if !strings.Contains("|String|Number|Boolean|parseInt|parseFloat|", "|"+name+"|") {
					return "", fmt.Errorf("函数不在白名单")
				}
				callee = name
			case *ast.DotExpression:
				name := c.Identifier.Name.String()
				if id, ok := c.Left.(*ast.Identifier); ok && id.Name.String() == "Math" {
					if !strings.Contains("|abs|round|floor|ceil|min|max|", "|"+name+"|") {
						return "", fmt.Errorf("Math函数不在白名单")
					}
					callee = "Math." + name
				} else {
					if !strings.Contains("|trim|toLowerCase|toUpperCase|slice|substring|replace|split|join|", "|"+name+"|") {
						return "", fmt.Errorf("方法不在白名单")
					}
					left, e := sub(c.Left)
					if e != nil {
						return "", e
					}
					callee = "(" + left + ")." + name
				}
			default:
				return "", fmt.Errorf("禁止动态函数调用")
			}
			args := []string{}
			for _, arg := range n.ArgumentList {
				v, e := sub(arg)
				if e != nil {
					return "", e
				}
				args = append(args, v)
			}
			text = callee + "(" + strings.Join(args, ",") + ")"
		default:
			return "", fmt.Errorf("仅支持纯表达式；禁止循环、函数、赋值、正则、动态代码及外部访问")
		}
		return "__bounded(" + text + ")", nil
	}
	emitted, e := emit(statement.Expression, 0)
	if e != nil {
		return nil, e
	}
	return goja.Compile("co.transform.v1", emitted, true)
}
