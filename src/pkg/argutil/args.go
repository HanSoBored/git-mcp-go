package argutil

import (
	"encoding/json"
	"fmt"
)

// GetInt extracts an integer from a map[string]interface{}.
// Returns the value if found and is a number, otherwise returns defaultValue.
// Note: zero is a valid value and will be returned.
func GetInt(args map[string]interface{}, key string, defaultValue int) int {
	if val, ok := args[key]; ok {
		switch v := val.(type) {
		case float64:
			return int(v)
		case json.Number:
			if i64, err := v.Int64(); err == nil {
				return int(i64)
			}
		}
	}
	return defaultValue
}

// GetString extracts a string from a map[string]interface{}
// Returns the value and a boolean indicating if it was found
func GetString(args map[string]interface{}, key string) (string, bool) {
	if val, ok := args[key].(string); ok {
		return val, true
	}
	return "", false
}

// GetRequiredString extracts a required string from a map[string]interface{}.
// Returns an error if the key is missing or not a string.
func GetRequiredString(args map[string]interface{}, key string) (string, error) {
	val, ok := args[key].(string)
	if !ok {
		return "", fmt.Errorf("missing required argument: %s", key)
	}
	return val, nil
}

// GetBool extracts a boolean from a map[string]interface{}
// Returns the value if found, otherwise returns defaultValue
func GetBool(args map[string]interface{}, key string, defaultValue bool) bool {
	if val, ok := args[key].(bool); ok {
		return val
	}
	return defaultValue
}

// GetStringArray extracts a string array from a map[string]interface{}
// Returns the array and an error if the type is invalid
func GetStringArray(args map[string]interface{}, key string) ([]string, error) {
	arrInterface, ok := args[key].([]interface{})
	if !ok {
		val, exists := args[key]
		if !exists {
			return nil, fmt.Errorf("key %q not found", key)
		}
		return nil, fmt.Errorf("key %q: expected array, got %T", key, val)
	}
	result := make([]string, len(arrInterface))
	for i, item := range arrInterface {
		if s, ok := item.(string); ok {
			result[i] = s
		} else {
			return nil, fmt.Errorf("key %q: element at index %d is not a string", key, i)
		}
	}
	return result, nil
}
