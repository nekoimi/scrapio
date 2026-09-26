package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/nekoimi/scrapio/internal/script"
)

type ScriptContract struct {
	InputFields  []string
	OutputFields []string
}

func contractFields(raw any, name string) ([]string, error) {
	list, ok := raw.([]any)
	if !ok || len(list) == 0 || len(list) > 100 {
		return nil, fmt.Errorf("%s: declare 1–100 field names", name)
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(list))
	for _, value := range list {
		field, ok := value.(string)
		if !ok || field == "" || seen[field] {
			return nil, fmt.Errorf("%s: invalid or duplicate field", name)
		}
		seen[field] = true
		out = append(out, field)
	}
	return out, nil
}

func scriptContract(config map[string]any) (ScriptContract, error) {
	var c ScriptContract
	var err error
	c.InputFields, err = contractFields(config["input_fields"], "input_fields")
	if err != nil {
		return c, err
	}
	c.OutputFields, err = contractFields(config["output_fields"], "output_fields")
	return c, err
}

func ExecuteRecordScript(ctx context.Context, config map[string]any, values map[string]any) (map[string]any, error) {
	c, err := scriptContract(config)
	if err != nil {
		return nil, err
	}
	input := map[string]any{}
	for _, field := range c.InputFields {
		value, ok := values[field]
		if !ok {
			return nil, fmt.Errorf("script input field %q is missing", field)
		}
		input[field] = value
	}
	timeout := script.DefaultTimeout
	if value, ok := config["timeout_ms"].(float64); ok {
		timeout = time.Duration(value) * time.Millisecond
	}
	result, err := script.Execute(ctx, script.Request{Script: stringValue(config["script"]), Input: input, Timeout: timeout})
	if err != nil {
		return nil, err
	}
	output, ok := result.Output.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("script output must be an object")
	}
	if len(output) != len(c.OutputFields) {
		return nil, fmt.Errorf("script output fields do not match declared contract")
	}
	for _, field := range c.OutputFields {
		if _, ok := output[field]; !ok {
			return nil, fmt.Errorf("script output field %q is missing", field)
		}
	}
	return output, nil
}
