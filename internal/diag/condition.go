package diag

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/SlimaineB/hop/internal/config"
)

type condition struct {
	all    []*condition
	any    []*condition
	not    *condition
	field  string
	op     string
	value  any
	values []any
	re     *regexp.Regexp
}

func compileCondition(spec config.DiagnosticCondition) (*condition, error) {
	cond := &condition{
		field:  strings.TrimSpace(spec.Field),
		op:     normalizeConditionOp(spec.Op),
		value:  spec.Value,
		values: append([]any(nil), spec.Values...),
	}
	for _, child := range spec.All {
		compiled, err := compileCondition(child)
		if err != nil {
			return nil, err
		}
		cond.all = append(cond.all, compiled)
	}
	for _, child := range spec.Any {
		compiled, err := compileCondition(child)
		if err != nil {
			return nil, err
		}
		cond.any = append(cond.any, compiled)
	}
	if spec.Not != nil {
		compiled, err := compileCondition(*spec.Not)
		if err != nil {
			return nil, err
		}
		cond.not = compiled
	}
	if cond.field == "" && len(cond.all) == 0 && len(cond.any) == 0 && cond.not == nil {
		return nil, fmt.Errorf("diagnostic condition needs field, all, any or not")
	}
	if cond.field != "" {
		if cond.op == "" {
			return nil, fmt.Errorf("diagnostic condition %q needs an op", cond.field)
		}
		if cond.op == "matches" {
			pattern := stringify(cond.value)
			if pattern == "" {
				return nil, fmt.Errorf("diagnostic condition %q: matches needs a pattern", cond.field)
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, fmt.Errorf("diagnostic condition %q: %w", cond.field, err)
			}
			cond.re = re
		}
	}
	return cond, nil
}

func normalizeConditionOp(op string) string {
	op = strings.ToLower(strings.TrimSpace(op))
	switch op {
	case "", "eq", "equals", "==":
		return "eq"
	case "ne", "neq", "!=":
		return "ne"
	case "in":
		return "in"
	case "not_in", "notin", "nin":
		return "not_in"
	case "contains":
		return "contains"
	case "not_contains", "notcontains":
		return "not_contains"
	case "matches", "regex", "regexp":
		return "matches"
	case "exists":
		return "exists"
	case "not_exists", "missing":
		return "not_exists"
	case "gt", ">":
		return "gt"
	case "gte", "ge", ">=":
		return "gte"
	case "lt", "<":
		return "lt"
	case "lte", "le", "<=":
		return "lte"
	case "starts_with", "prefix":
		return "starts_with"
	case "ends_with", "suffix":
		return "ends_with"
	case "empty":
		return "empty"
	case "not_empty", "nonempty":
		return "not_empty"
	default:
		return op
	}
}

func (c *condition) evaluate(facts map[string]any) (bool, error) {
	if c == nil {
		return true, nil
	}
	for _, child := range c.all {
		ok, err := child.evaluate(facts)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	if len(c.any) > 0 {
		matched := false
		for _, child := range c.any {
			ok, err := child.evaluate(facts)
			if err != nil {
				return false, err
			}
			if ok {
				matched = true
				break
			}
		}
		if !matched {
			return false, nil
		}
	}
	if c.not != nil {
		ok, err := c.not.evaluate(facts)
		if err != nil {
			return false, err
		}
		if ok {
			return false, nil
		}
	}
	if c.field == "" {
		return true, nil
	}
	actual, exists := lookupFact(facts, c.field)
	return c.evaluateLeaf(actual, exists)
}

func (c *condition) evaluateLeaf(actual any, exists bool) (bool, error) {
	switch c.op {
	case "exists":
		return exists, nil
	case "not_exists":
		return !exists, nil
	}
	if !exists {
		return false, nil
	}
	switch c.op {
	case "eq":
		return anyEqual(actual, c.value), nil
	case "ne":
		return !anyEqual(actual, c.value), nil
	case "in", "not_in":
		expected := c.values
		if len(expected) == 0 && c.value != nil {
			expected = []any{c.value}
		}
		found := anyIn(actual, expected)
		if c.op == "in" {
			return found, nil
		}
		return !found, nil
	case "contains", "not_contains":
		found := anyContains(actual, c.value)
		if c.op == "contains" {
			return found, nil
		}
		return !found, nil
	case "matches":
		return anyMatches(actual, c.re), nil
	case "gt", "gte", "lt", "lte":
		left, ok := firstNumber(actual)
		right, ok2 := firstNumber(c.value)
		if !ok || !ok2 {
			return false, nil
		}
		switch c.op {
		case "gt":
			return left > right, nil
		case "gte":
			return left >= right, nil
		case "lt":
			return left < right, nil
		default:
			return left <= right, nil
		}
	case "starts_with", "ends_with":
		needle := stringify(c.value)
		for _, value := range flatten(actual) {
			text := stringify(value)
			if c.op == "starts_with" && strings.HasPrefix(text, needle) {
				return true, nil
			}
			if c.op == "ends_with" && strings.HasSuffix(text, needle) {
				return true, nil
			}
		}
		return false, nil
	case "empty":
		return isEmpty(actual), nil
	case "not_empty":
		return !isEmpty(actual), nil
	default:
		return false, fmt.Errorf("unknown diagnostic operator %q", c.op)
	}
}

func lookupFact(facts map[string]any, path string) (any, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, false
	}
	parts := strings.Split(path, ".")
	var current any = facts
	for i, part := range parts {
		switch node := current.(type) {
		case map[string]any:
			value, ok := node[part]
			if !ok && i < len(parts)-1 {
				value, ok = node[strings.Join(parts[i:], ".")]
				if ok {
					return value, true
				}
			}
			if !ok {
				return nil, false
			}
			current = value
		case map[string]string:
			value, ok := node[part]
			if !ok && i < len(parts)-1 {
				value, ok = node[strings.Join(parts[i:], ".")]
				if ok {
					return value, true
				}
			}
			if !ok {
				return nil, false
			}
			current = value
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		case []string:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) {
				return nil, false
			}
			current = node[index]
		default:
			return nil, false
		}
	}
	return current, true
}

func anyEqual(actual, expected any) bool {
	for _, value := range flatten(actual) {
		if equalScalar(value, expected) {
			return true
		}
	}
	return false
}

func anyIn(actual any, expected []any) bool {
	for _, value := range flatten(actual) {
		for _, candidate := range expected {
			if equalScalar(value, candidate) {
				return true
			}
		}
	}
	return false
}

func anyContains(actual, expected any) bool {
	needle := stringify(expected)
	for _, value := range flatten(actual) {
		if text, ok := value.(string); ok && strings.Contains(text, needle) {
			return true
		}
		if equalScalar(value, expected) {
			return true
		}
	}
	return false
}

func anyMatches(actual any, re *regexp.Regexp) bool {
	if re == nil {
		return false
	}
	for _, value := range flatten(actual) {
		if re.MatchString(stringify(value)) {
			return true
		}
	}
	return false
}

func equalScalar(left, right any) bool {
	if left == nil || right == nil {
		return left == right
	}
	if leftNumber, ok := toFloat(left); ok {
		if rightNumber, ok := toFloat(right); ok {
			return leftNumber == rightNumber
		}
	}
	leftValue := reflect.ValueOf(left)
	rightValue := reflect.ValueOf(right)
	if leftValue.IsValid() && rightValue.IsValid() && leftValue.Kind() == rightValue.Kind() {
		return reflect.DeepEqual(left, right)
	}
	return stringify(left) == stringify(right)
}

func firstNumber(value any) (float64, bool) {
	for _, item := range flatten(value) {
		if number, ok := toFloat(item); ok {
			return number, true
		}
	}
	return 0, false
}

func toFloat(value any) (float64, bool) {
	switch number := value.(type) {
	case int:
		return float64(number), true
	case int8:
		return float64(number), true
	case int16:
		return float64(number), true
	case int32:
		return float64(number), true
	case int64:
		return float64(number), true
	case uint:
		return float64(number), true
	case uint8:
		return float64(number), true
	case uint16:
		return float64(number), true
	case uint32:
		return float64(number), true
	case uint64:
		return float64(number), true
	case float32:
		return float64(number), true
	case float64:
		return number, true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(number), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func flatten(value any) []any {
	if value == nil {
		return []any{nil}
	}
	switch node := value.(type) {
	case []any:
		return node
	case []string:
		out := make([]any, 0, len(node))
		for _, item := range node {
			out = append(out, item)
		}
		return out
	case []int:
		out := make([]any, 0, len(node))
		for _, item := range node {
			out = append(out, item)
		}
		return out
	default:
		return []any{value}
	}
}

func isEmpty(value any) bool {
	if value == nil {
		return true
	}
	switch node := value.(type) {
	case string:
		return strings.TrimSpace(node) == ""
	case []any:
		return len(node) == 0
	case []string:
		return len(node) == 0
	case map[string]any:
		return len(node) == 0
	case map[string]string:
		return len(node) == 0
	default:
		return false
	}
}

func stringify(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return fmt.Sprint(value)
}
