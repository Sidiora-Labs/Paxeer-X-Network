package httpapi

import "fmt"

// asString coerces a decoded JSON value (string, number, or json.Number) into a
// string, mirroring the loose coercion the TS candles webhook applies to event
// args. nil and unknown types yield the empty string.
func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%v", val)
	case fmt.Stringer:
		return val.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// asBool coerces a decoded JSON value into a bool. Native bools pass through;
// the string "true" is treated as true; everything else is false.
func asBool(v interface{}) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "true"
	default:
		return false
	}
}
