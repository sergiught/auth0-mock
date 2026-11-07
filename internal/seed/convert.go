package seed

import "fmt"

// toJSONValue turns a value decoded by yaml.Unmarshal into any into one
// encoding/json can marshal: map keys become strings (YAML allows 200 or
// true as a key) and nested collections are converted recursively.
func toJSONValue(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			c, err := toJSONValue(val)
			if err != nil {
				return nil, err
			}
			out[k] = c
		}
		return out, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			switch k.(type) {
			case map[string]any, map[any]any, []any:
				return nil, fmt.Errorf("unsupported map key %v: keys must be scalars", k)
			}
			c, err := toJSONValue(val)
			if err != nil {
				return nil, err
			}
			key := fmt.Sprint(k)
			if _, dup := out[key]; dup {
				return nil, fmt.Errorf("duplicate key %q after conversion to text", key)
			}
			out[key] = c
		}
		return out, nil
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			c, err := toJSONValue(val)
			if err != nil {
				return nil, err
			}
			out[i] = c
		}
		return out, nil
	default:
		return v, nil
	}
}
