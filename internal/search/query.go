package search

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

type FieldKind string

const (
	FieldPod       FieldKind = "pod"
	FieldNamespace FieldKind = "namespace"
	FieldContext   FieldKind = "context"
	FieldCluster   FieldKind = "cluster"
	FieldEnv       FieldKind = "env"
	FieldOwner     FieldKind = "owner"
	FieldNode      FieldKind = "node"
	FieldLabel     FieldKind = "label"
	FieldContainer FieldKind = "container"
	FieldImage     FieldKind = "image"
	FieldPhase     FieldKind = "phase"
	FieldServer    FieldKind = "server"
	FieldTag       FieldKind = "tag"
	FieldHost      FieldKind = "host"
	FieldUser      FieldKind = "user"
	FieldSSH       FieldKind = "ssh"
)

var fieldAliases = map[string]FieldKind{
	"pod":         FieldPod,
	"pods":        FieldPod,
	"name":        FieldPod,
	"ns":          FieldNamespace,
	"namespace":   FieldNamespace,
	"ctx":         FieldContext,
	"context":     FieldContext,
	"cluster":     FieldCluster,
	"env":         FieldEnv,
	"environment": FieldEnv,
	"owner":       FieldOwner,
	"workload":    FieldOwner,
	"deploy":      FieldOwner,
	"node":        FieldNode,
	"label":       FieldLabel,
	"labels":      FieldLabel,
	"lbl":         FieldLabel,
	"container":   FieldContainer,
	"image":       FieldImage,
	"phase":       FieldPhase,
	"status":      FieldPhase,
	"server":      FieldServer,
	"servers":     FieldServer,
	"srv":         FieldServer,
	"tag":         FieldTag,
	"tags":        FieldTag,
	"host":        FieldHost,
	"hostname":    FieldHost,
	"user":        FieldUser,
	"ssh":         FieldSSH,
}

type TimeRange struct {
	From time.Time `json:"from"`
	To   time.Time `json:"to"`
	Raw  []string  `json:"raw,omitempty"`
}

type Query struct {
	Raw       string                 `json:"raw"`
	Terms     []string               `json:"terms,omitempty"`
	Filters   map[FieldKind][]string `json:"filters,omitempty"`
	TimeRange *TimeRange             `json:"time_range,omitempty"`
}

func Parse(raw string) Query {
	return ParseAt(raw, time.Now())
}

func ParseAt(raw string, now time.Time) Query {
	q := Query{Raw: raw, Filters: map[FieldKind][]string{}}
	var times []parsedTime
	var explicitFrom, explicitTo *parsedTime

	for _, token := range splitTokens(raw) {
		key, value, hasKey := strings.Cut(token, ":")
		if hasKey && value != "" {
			lowerKey := strings.ToLower(key)
			switch lowerKey {
			case "from", "since", "after":
				if parsed, ok := parseTimeToken(value, now); ok {
					explicitFrom = &parsed
					continue
				}
			case "to", "until", "before":
				if parsed, ok := parseTimeToken(value, now); ok {
					explicitTo = &parsed
					continue
				}
			}
			if kind, exists := fieldAliases[lowerKey]; exists {
				q.Filters[kind] = append(q.Filters[kind], value)
				continue
			}
		}
		if parsed, ok := parseTimeToken(token, now); ok {
			times = append(times, parsed)
			continue
		}
		q.Terms = append(q.Terms, token)
	}

	q.TimeRange = buildTimeRange(times, explicitFrom, explicitTo, now)
	return q
}

func (q Query) Empty() bool {
	return len(q.Terms) == 0 && len(q.Filters) == 0 && q.TimeRange == nil
}

type parsedTime struct {
	value     time.Time
	raw       string
	relative  bool
	clockOnly bool
	hasDate   bool
}

var (
	clockHourPattern  = regexp.MustCompile(`^(\d{1,2})h(\d{2})?$`)
	clockColonPattern = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
	dayMonthPattern   = regexp.MustCompile(`^(\d{1,2})/(\d{1,2})(?:/(\d{2,4}))?$`)
	relativePattern   = regexp.MustCompile(`^(\d+(?:\.\d+)?)([smhdw])$`)
)

func parseTimeToken(token string, now time.Time) (parsedTime, bool) {
	value := strings.TrimSpace(token)
	if value == "" {
		return parsedTime{}, false
	}
	lower := strings.ToLower(value)
	switch lower {
	case "now":
		return parsedTime{value: now, raw: token, relative: true}, true
	case "today":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		return parsedTime{value: start, raw: token, hasDate: true}, true
	case "yesterday":
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
		return parsedTime{value: start, raw: token, hasDate: true}, true
	}

	if strings.HasPrefix(lower, "last:") {
		if duration, ok := parseRelativeDuration(strings.TrimPrefix(lower, "last:")); ok {
			return parsedTime{value: now.Add(-duration), raw: token, relative: true}, true
		}
	}
	if strings.HasPrefix(value, "-") {
		if duration, ok := parseRelativeDuration(value[1:]); ok {
			return parsedTime{value: now.Add(-duration), raw: token, relative: true}, true
		}
	}

	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		parsed, err := time.ParseInLocation(layout, value, now.Location())
		if err == nil {
			if layout == "2006-01-02" {
				parsed = time.Date(parsed.Year(), parsed.Month(), parsed.Day(), 0, 0, 0, 0, now.Location())
			}
			return parsedTime{value: parsed, raw: token, hasDate: true}, true
		}
	}

	if match := clockHourPattern.FindStringSubmatch(value); match != nil {
		hour, _ := strconv.Atoi(match[1])
		minute := 0
		if match[2] != "" {
			minute, _ = strconv.Atoi(match[2])
		}
		if clock, ok := clockTime(now, hour, minute); ok {
			return parsedTime{value: clock, raw: token, clockOnly: true}, true
		}
	}
	if match := clockColonPattern.FindStringSubmatch(value); match != nil {
		hour, _ := strconv.Atoi(match[1])
		minute, _ := strconv.Atoi(match[2])
		if clock, ok := clockTime(now, hour, minute); ok {
			return parsedTime{value: clock, raw: token, clockOnly: true}, true
		}
	}
	if match := dayMonthPattern.FindStringSubmatch(value); match != nil {
		day, _ := strconv.Atoi(match[1])
		month, _ := strconv.Atoi(match[2])
		year := now.Year()
		if match[3] != "" {
			year, _ = strconv.Atoi(match[3])
			if year < 100 {
				year += 2000
			}
		}
		if month >= 1 && month <= 12 && day >= 1 && day <= 31 {
			parsed := time.Date(year, time.Month(month), day, 0, 0, 0, 0, now.Location())
			if parsed.Day() == day && parsed.Month() == time.Month(month) {
				return parsedTime{value: parsed, raw: token, hasDate: true}, true
			}
		}
	}
	return parsedTime{}, false
}

func clockTime(now time.Time, hour, minute int) (time.Time, bool) {
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return time.Time{}, false
	}
	return time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location()), true
}

func parseRelativeDuration(value string) (time.Duration, bool) {
	match := relativePattern.FindStringSubmatch(strings.ToLower(strings.TrimSpace(value)))
	if match == nil {
		return 0, false
	}
	amount, err := strconv.ParseFloat(match[1], 64)
	if err != nil || amount <= 0 {
		return 0, false
	}
	var unit time.Duration
	switch match[2] {
	case "s":
		unit = time.Second
	case "m":
		unit = time.Minute
	case "h":
		unit = time.Hour
	case "d":
		unit = 24 * time.Hour
	case "w":
		unit = 7 * 24 * time.Hour
	default:
		return 0, false
	}
	return time.Duration(amount * float64(unit)), true
}

func buildTimeRange(times []parsedTime, explicitFrom, explicitTo *parsedTime, now time.Time) *TimeRange {
	if len(times) == 0 && explicitFrom == nil && explicitTo == nil {
		return nil
	}
	raw := make([]string, 0, len(times)+2)
	for _, parsed := range times {
		raw = append(raw, parsed.raw)
	}
	if explicitFrom != nil {
		raw = append(raw, explicitFrom.raw)
	}
	if explicitTo != nil {
		raw = append(raw, explicitTo.raw)
	}

	var start, end time.Time
	switch {
	case explicitFrom != nil && explicitTo != nil:
		start = explicitFrom.value
		end = explicitTo.value
		if end.Before(start) {
			start, end = end, start
		}
	case explicitFrom != nil:
		start = explicitFrom.value
		if explicitFrom.relative || !start.After(now) {
			end = now
		} else {
			end = start.Add(time.Hour)
		}
		if end.Before(start) {
			end = start.Add(time.Hour)
		}
	case explicitTo != nil:
		end = explicitTo.value
		start = end.Add(-time.Hour)
		if end.After(now) && start.Before(now) {
			start = now
		}
	case len(times) >= 2:
		start, end = orderTimePair(times[0], times[1])
	case len(times) == 1:
		single := times[0]
		start = single.value
		if single.relative {
			end = now
		} else if start.After(now) {
			end = start.Add(time.Hour)
		} else {
			end = now
		}
		if end.Before(start) {
			end = start.Add(time.Hour)
		}
	default:
		return nil
	}

	if start.IsZero() || end.IsZero() {
		return nil
	}
	if end.Before(start) {
		start, end = end, start
	}
	return &TimeRange{From: start, To: end, Raw: raw}
}

func orderTimePair(first, second parsedTime) (time.Time, time.Time) {
	if first.clockOnly && second.clockOnly {
		forward := second.value.Sub(first.value)
		if forward < 0 {
			forward += 24 * time.Hour
		}
		backward := first.value.Sub(second.value)
		if backward < 0 {
			backward += 24 * time.Hour
		}
		if forward <= backward {
			return first.value, first.value.Add(forward)
		}
		return second.value, second.value.Add(backward)
	}
	if second.value.Before(first.value) {
		return second.value, first.value
	}
	return first.value, second.value
}

func splitTokens(input string) []string {
	var tokens []string
	var current strings.Builder
	var quote rune
	flush := func() {
		if current.Len() > 0 {
			tokens = append(tokens, current.String())
			current.Reset()
		}
	}
	for _, r := range input {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			current.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t' || r == '\n':
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	return tokens
}
