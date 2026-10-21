package broker

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// frozenStreamSchemas is the exact reachable subset of the frozen AsyncAPI
// schemas, retained byte-for-value with no schema relaxation.
//
//go:embed frozen_stream_schemas.json
var frozenStreamSchemas []byte
var streamSchemas = func() map[string]map[string]any {
	var s map[string]map[string]any
	if err := json.Unmarshal(frozenStreamSchemas, &s); err != nil {
		panic(err)
	}
	return s
}()
var families = []string{"BountiesEvent", "CandleEvent", "MarketPriceEvent", "MarketTradesEvent", "MessagesEvent", "NotificationsEvent", "OrdersEvent", "PortfolioEvent", "RankingEvent", "RewardsEvent", "StatsEvent", "StreamsEvent"}

// validateSchema implements every assertion keyword in the frozen stream
// subset. Unsupported assertion/type/format fails closed. Integers are decoded
// as json.Number and never pass through float64.
func validateSchema(s map[string]any, v any, depth int) error {
	if depth > 64 {
		return ErrFrame
	}
	fail := func() error { return ErrFrame }
	if ref, ok := s["$ref"].(string); ok {
		target, ok := streamSchemas[strings.TrimPrefix(ref, "#/components/schemas/")]
		if !ok {
			return fail()
		}
		return validateSchema(target, v, depth+1)
	}
	if variants, ok := s["anyOf"].([]any); ok {
		for _, x := range variants {
			if validateSchema(x.(map[string]any), v, depth+1) == nil {
				return nil
			}
		}
		return fail()
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		return fail()
	}
	if values, ok := s["enum"].([]any); ok {
		found := false
		for _, x := range values {
			if reflect.DeepEqual(x, v) {
				found = true
			}
		}
		if !found {
			return fail()
		}
	}
	typ, ok := s["type"].(string)
	if !ok {
		return fail()
	}
	switch typ {
	case "null":
		if v != nil {
			return fail()
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fail()
		}
	case "string":
		x, ok := v.(string)
		if !ok {
			return fail()
		}
		n := utf8.RuneCountInString(x)
		if a, ok := s["minLength"].(float64); ok && n < int(a) {
			return fail()
		}
		if a, ok := s["maxLength"].(float64); ok && n > int(a) {
			return fail()
		}
		if p, ok := s["pattern"].(string); ok {

			r, err := regexp.Compile(p)
			if err != nil || !r.MatchString(x) {
				return fail()
			}
		}
		if f, ok := s["format"].(string); ok {
			switch f {
			case "date-time":
				t, err := time.Parse("2006-01-02T15:04:05.000Z", x)
				if err != nil || t.Format("2006-01-02T15:04:05.000Z") != x {
					return fail()
				}
			case "uuid":
				if !regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`).MatchString(x) {
					return fail()
				}
			case "uri":
				if !strings.HasPrefix(x, "https://") {
					return fail()
				}
			default:
				return fail()
			}
		}
	case "integer":
		x, ok := v.(json.Number)
		if !ok {
			return fail()
		}
		n, ok := new(big.Int).SetString(string(x), 10)
		if !ok {
			return fail()
		}
		for _, bound := range []string{"minimum", "maximum"} {
			if a, ok := s[bound].(float64); ok {
				b := big.NewInt(int64(a))
				cmp := n.Cmp(b)
				if bound == "minimum" && cmp < 0 || bound == "maximum" && cmp > 0 {
					return fail()
				}
			}
		}
	case "object":
		x, ok := v.(map[string]any)
		if !ok {
			return fail()
		}
		props, _ := s["properties"].(map[string]any)
		if req, ok := s["required"].([]any); ok {
			for _, k := range req {
				if _, ok := x[k.(string)]; !ok {
					return fail()
				}
			}
		}
		for k, value := range x {
			if sub, ok := props[k].(map[string]any); ok {
				if err := validateSchema(sub, value, depth+1); err != nil {
					return fmt.Errorf("%s: %w", k, err)
				}
			} else if s["additionalProperties"] == false {
				return fail()
			}
		}
	case "array":
		x, ok := v.([]any)
		if !ok {
			return fail()
		}
		if a, ok := s["minItems"].(float64); ok && len(x) < int(a) {
			return fail()
		}
		if a, ok := s["maxItems"].(float64); ok && len(x) > int(a) {
			return fail()
		}
		sub, ok := s["items"].(map[string]any)
		if !ok {
			return fail()
		}
		for _, value := range x {
			if err := validateSchema(sub, value, depth+1); err != nil {
				return err
			}
		}
	default:
		return fail()
	}
	return nil
}
func validateFrozenFrame(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value any
	if dec.Decode(&value) != nil {
		return ErrFrame
	}
	if dec.Decode(new(any)) != io.EOF {
		return ErrFrame
	}
	obj, ok := value.(map[string]any)
	if !ok {
		return ErrFrame
	}
	topic, ok := obj["topic"].(string)
	if !ok {
		return ErrFrame
	}
	for _, family := range families {
		schema := streamSchemas[family]
		properties := schema["properties"].(map[string]any)
		ts := properties["topic"].(map[string]any)
		topicSchema := make(map[string]any, len(ts))
		for k, v := range ts {
			topicSchema[k] = v
		}
		topicSchema["pattern"] = ts["pattern"]
		if validateSchema(topicSchema, topic, 0) == nil {
			boundSchema := make(map[string]any, len(schema))
			for k, v := range schema {
				boundSchema[k] = v
			}
			boundProps := make(map[string]any, len(properties))
			for k, v := range properties {
				boundProps[k] = v
			}
			boundProps["topic"] = topicSchema
			boundSchema["properties"] = boundProps
			if err := validateSchema(boundSchema, value, 0); err != nil {
				return err
			}
			return validateTopicBinding(family, topic, obj["payload"].(map[string]any))
		}
	}
	return ErrFrame
}
func validateTopicBinding(family, topic string, p map[string]any) error {
	parts := strings.Split(topic, ".")
	var key string
	index := len(parts) - 1
	switch family {
	case "CandleEvent":
		key = "poolAddress"
		index = 2
		if p["interval"] != parts[3] {
			return ErrFrame
		}
	case "MarketTradesEvent", "StatsEvent":
		key = "poolAddress"
	case "MessagesEvent":
		key = "roomId"
		index = 1
	case "PortfolioEvent":
		key = "wallet"
		p = p["summary"].(map[string]any)
	case "NotificationsEvent", "OrdersEvent", "RewardsEvent":
		key = "wallet"
	case "RankingEvent":
		key = "category"
	case "BountiesEvent":
		key = "bountyId"
		index = 1
	case "StreamsEvent":
		key = "streamId"
		index = 1
	}
	if key != "" {
		v, ok := p[key].(string)
		matches := v == parts[index]
		if key != "category" {
			matches = strings.EqualFold(v, parts[index])
		}
		if !ok || !matches {
			return ErrFrame
		}
	}
	return nil
}
