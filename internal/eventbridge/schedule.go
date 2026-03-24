package eventbridge

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	rateKind = "rate"
	cronKind = "cron"
)

var (
	rateExprRE = regexp.MustCompile(`(?i)^rate\(\s*([0-9]+)\s+(minute|minutes|hour|hours|day|days)\s*\)$`)
	cronExprRE = regexp.MustCompile(`(?i)^cron\((.+)\)$`)
)

var monthNames = map[string]int{
	"JAN": 1,
	"FEB": 2,
	"MAR": 3,
	"APR": 4,
	"MAY": 5,
	"JUN": 6,
	"JUL": 7,
	"AUG": 8,
	"SEP": 9,
	"OCT": 10,
	"NOV": 11,
	"DEC": 12,
}

var dowNames = map[string]int{
	"SUN": 1,
	"MON": 2,
	"TUE": 3,
	"WED": 4,
	"THU": 5,
	"FRI": 6,
	"SAT": 7,
}

type compiledSchedule struct {
	kind         string
	rateInterval time.Duration
	cron         *cronSchedule
}

type cronSchedule struct {
	minutes simpleField
	hours   simpleField
	months  simpleField
	years   simpleField
	dom     dayOfMonthField
	dow     dayOfWeekField
}

type simpleField struct {
	any    bool
	values map[int]struct{}
}

type dayOfMonthField struct {
	noSpec       bool
	any          bool
	values       map[int]struct{}
	lastDay      bool
	lastWeekday  bool
	nearestWeekd []int
}

type nthWeekday struct {
	weekday int
	nth     int
}

type dayOfWeekField struct {
	noSpec       bool
	any          bool
	values       map[int]struct{}
	lastWeekdays []int
	nthWeekdays  []nthWeekday
}

func validateScheduleExpression(expr string) error {
	_, err := parseScheduleExpression(expr)
	return err
}

func parseScheduleExpression(expr string) (*compiledSchedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("parameter ScheduleExpression is required")
	}

	if m := rateExprRE.FindStringSubmatch(expr); m != nil {
		interval, err := parseRateExpression(m[1], m[2])
		if err != nil {
			return nil, err
		}
		return &compiledSchedule{kind: rateKind, rateInterval: interval}, nil
	}

	if m := cronExprRE.FindStringSubmatch(expr); m != nil {
		cron, err := parseCronFields(m[1])
		if err != nil {
			return nil, err
		}
		return &compiledSchedule{kind: cronKind, cron: cron}, nil
	}

	return nil, fmt.Errorf("invalid ScheduleExpression %q: expected rate(...) or cron(...)", expr)
}

func parseRateExpression(valuePart, unitPart string) (time.Duration, error) {
	value, err := strconv.Atoi(valuePart)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("invalid rate value %q", valuePart)
	}

	unit := strings.ToLower(unitPart)
	if value == 1 {
		if strings.HasSuffix(unit, "s") {
			return 0, fmt.Errorf("rate(1 ...) must use singular unit")
		}
	} else if !strings.HasSuffix(unit, "s") {
		return 0, fmt.Errorf("rate(%d ...) must use plural unit", value)
	}

	singular := strings.TrimSuffix(unit, "s")
	switch singular {
	case "minute":
		return time.Duration(value) * time.Minute, nil
	case "hour":
		return time.Duration(value) * time.Hour, nil
	case "day":
		return time.Duration(value) * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("invalid rate unit %q", unitPart)
	}
}

func parseCronFields(raw string) (*cronSchedule, error) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) != 6 {
		return nil, fmt.Errorf("invalid cron expression: expected 6 fields, got %d", len(parts))
	}

	minutes, err := parseSimpleField(parts[0], 0, 59, nil, false)
	if err != nil {
		return nil, fmt.Errorf("invalid cron minute field: %w", err)
	}
	hours, err := parseSimpleField(parts[1], 0, 23, nil, false)
	if err != nil {
		return nil, fmt.Errorf("invalid cron hour field: %w", err)
	}
	dom, err := parseDayOfMonthField(parts[2])
	if err != nil {
		return nil, fmt.Errorf("invalid cron day-of-month field: %w", err)
	}
	months, err := parseSimpleField(parts[3], 1, 12, monthNames, false)
	if err != nil {
		return nil, fmt.Errorf("invalid cron month field: %w", err)
	}
	dow, err := parseDayOfWeekField(parts[4])
	if err != nil {
		return nil, fmt.Errorf("invalid cron day-of-week field: %w", err)
	}
	years, err := parseSimpleField(parts[5], 1970, 2199, nil, false)
	if err != nil {
		return nil, fmt.Errorf("invalid cron year field: %w", err)
	}

	if dom.noSpec == dow.noSpec {
		return nil, fmt.Errorf("day-of-month and day-of-week cannot both be specified or both be ?")
	}

	return &cronSchedule{
		minutes: minutes,
		hours:   hours,
		months:  months,
		years:   years,
		dom:     dom,
		dow:     dow,
	}, nil
}

func parseSimpleField(raw string, min, max int, names map[string]int, allowQuestion bool) (simpleField, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "" {
		return simpleField{}, fmt.Errorf("empty field")
	}
	if allowQuestion && raw == "?" {
		return simpleField{any: true}, nil
	}
	if raw == "*" {
		return simpleField{any: true}, nil
	}

	values := make(map[int]struct{})
	for _, segment := range strings.Split(raw, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return simpleField{}, fmt.Errorf("invalid list token")
		}
		expanded, err := expandSimpleToken(segment, min, max, names, false)
		if err != nil {
			return simpleField{}, err
		}
		for _, v := range expanded {
			values[v] = struct{}{}
		}
	}
	if len(values) == 0 {
		return simpleField{}, fmt.Errorf("no values")
	}
	return simpleField{values: values}, nil
}

func parseDayOfMonthField(raw string) (dayOfMonthField, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "?" {
		return dayOfMonthField{noSpec: true}, nil
	}

	field := dayOfMonthField{values: make(map[int]struct{})}
	for _, segment := range strings.Split(raw, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return dayOfMonthField{}, fmt.Errorf("invalid list token")
		}

		switch {
		case segment == "*":
			field.any = true
		case segment == "L":
			field.lastDay = true
		case segment == "LW":
			field.lastWeekday = true
		case strings.HasSuffix(segment, "W"):
			dayRaw := strings.TrimSuffix(segment, "W")
			day, err := strconv.Atoi(dayRaw)
			if err != nil {
				return dayOfMonthField{}, fmt.Errorf("invalid W token %q", segment)
			}
			if day < 1 || day > 31 {
				return dayOfMonthField{}, fmt.Errorf("w token out of range %q", segment)
			}
			field.nearestWeekd = append(field.nearestWeekd, day)
		case strings.ContainsAny(segment, "L#"):
			return dayOfMonthField{}, fmt.Errorf("unsupported day-of-month token %q", segment)
		default:
			expanded, err := expandSimpleToken(segment, 1, 31, nil, false)
			if err != nil {
				return dayOfMonthField{}, err
			}
			for _, v := range expanded {
				field.values[v] = struct{}{}
			}
		}
	}

	if !field.any && len(field.values) == 0 && !field.lastDay && !field.lastWeekday && len(field.nearestWeekd) == 0 {
		return dayOfMonthField{}, fmt.Errorf("day-of-month has no values")
	}
	return field, nil
}

func parseDayOfWeekField(raw string) (dayOfWeekField, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if raw == "?" {
		return dayOfWeekField{noSpec: true}, nil
	}

	field := dayOfWeekField{values: make(map[int]struct{})}
	for _, segment := range strings.Split(raw, ",") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return dayOfWeekField{}, fmt.Errorf("invalid list token")
		}

		switch {
		case segment == "*":
			field.any = true
		case segment == "L":
			field.lastWeekdays = append(field.lastWeekdays, 7)
		case strings.Contains(segment, "#"):
			parts := strings.SplitN(segment, "#", 2)
			if len(parts) != 2 {
				return dayOfWeekField{}, fmt.Errorf("invalid # token %q", segment)
			}
			weekday, err := parseNamedOrNumeric(parts[0], 1, 7, dowNames, true)
			if err != nil {
				return dayOfWeekField{}, fmt.Errorf("invalid # weekday %q", segment)
			}
			nth, err := strconv.Atoi(parts[1])
			if err != nil || nth < 1 || nth > 5 {
				return dayOfWeekField{}, fmt.Errorf("invalid # nth value %q", segment)
			}
			field.nthWeekdays = append(field.nthWeekdays, nthWeekday{weekday: weekday, nth: nth})
		case strings.HasSuffix(segment, "L"):
			weekdayRaw := strings.TrimSuffix(segment, "L")
			weekday, err := parseNamedOrNumeric(weekdayRaw, 1, 7, dowNames, true)
			if err != nil {
				return dayOfWeekField{}, fmt.Errorf("invalid L token %q", segment)
			}
			field.lastWeekdays = append(field.lastWeekdays, weekday)
		default:
			expanded, err := expandSimpleToken(segment, 1, 7, dowNames, true)
			if err != nil {
				return dayOfWeekField{}, err
			}
			for _, v := range expanded {
				field.values[v] = struct{}{}
			}
		}
	}

	if !field.any && len(field.values) == 0 && len(field.lastWeekdays) == 0 && len(field.nthWeekdays) == 0 {
		return dayOfWeekField{}, fmt.Errorf("day-of-week has no values")
	}

	return field, nil
}

func expandSimpleToken(token string, min, max int, names map[string]int, normalizeDow bool) ([]int, error) {
	token = strings.TrimSpace(strings.ToUpper(token))
	if token == "" {
		return nil, fmt.Errorf("empty token")
	}

	if strings.Contains(token, "/") {
		parts := strings.SplitN(token, "/", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid increment token %q", token)
		}
		step, err := strconv.Atoi(parts[1])
		if err != nil || step <= 0 {
			return nil, fmt.Errorf("invalid increment value in %q", token)
		}

		base := parts[0]
		start := min
		end := max
		if base != "*" {
			if strings.Contains(base, "-") {
				r, err := parseRange(base, min, max, names, normalizeDow)
				if err != nil {
					return nil, err
				}
				start, end = r[0], r[1]
			} else {
				v, err := parseNamedOrNumeric(base, min, max, names, normalizeDow)
				if err != nil {
					return nil, err
				}
				start = v
			}
		}

		vals := make([]int, 0)
		for v := start; v <= end; v += step {
			vals = append(vals, v)
		}
		if len(vals) == 0 {
			return nil, fmt.Errorf("empty increment set in %q", token)
		}
		return vals, nil
	}

	if strings.Contains(token, "-") {
		r, err := parseRange(token, min, max, names, normalizeDow)
		if err != nil {
			return nil, err
		}
		vals := make([]int, 0, r[1]-r[0]+1)
		for v := r[0]; v <= r[1]; v++ {
			vals = append(vals, v)
		}
		return vals, nil
	}

	v, err := parseNamedOrNumeric(token, min, max, names, normalizeDow)
	if err != nil {
		return nil, err
	}
	return []int{v}, nil
}

func parseRange(token string, min, max int, names map[string]int, normalizeDow bool) ([2]int, error) {
	parts := strings.SplitN(token, "-", 2)
	if len(parts) != 2 {
		return [2]int{}, fmt.Errorf("invalid range token %q", token)
	}
	start, err := parseNamedOrNumeric(parts[0], min, max, names, normalizeDow)
	if err != nil {
		return [2]int{}, err
	}
	end, err := parseNamedOrNumeric(parts[1], min, max, names, normalizeDow)
	if err != nil {
		return [2]int{}, err
	}
	if end < start {
		return [2]int{}, fmt.Errorf("invalid descending range %q", token)
	}
	return [2]int{start, end}, nil
}

func parseNamedOrNumeric(raw string, min, max int, names map[string]int, normalizeDow bool) (int, error) {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if names != nil {
		if v, ok := names[raw]; ok {
			return v, nil
		}
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q", raw)
	}
	if normalizeDow {
		if value == 0 {
			value = 1
		}
	}
	if value < min || value > max {
		return 0, fmt.Errorf("value %d out of range [%d,%d]", value, min, max)
	}
	return value, nil
}

func computeNextRun(expr string, anchor, after time.Time) (time.Time, error) {
	compiled, err := parseScheduleExpression(expr)
	if err != nil {
		return time.Time{}, err
	}

	switch compiled.kind {
	case rateKind:
		return nextRateRun(anchor, compiled.rateInterval, after), nil
	case cronKind:
		return compiled.cron.nextAfter(after)
	default:
		return time.Time{}, fmt.Errorf("unsupported schedule kind %q", compiled.kind)
	}
}

func nextRateRun(anchor time.Time, interval time.Duration, after time.Time) time.Time {
	if interval <= 0 {
		return after.UTC().Truncate(time.Minute).Add(time.Minute)
	}
	anchorUTC := anchor.UTC().Truncate(time.Minute)
	afterUTC := after.UTC().Truncate(time.Minute)
	if anchorUTC.After(afterUTC) {
		return anchorUTC
	}
	elapsed := afterUTC.Sub(anchorUTC)
	steps := elapsed/interval + 1
	return anchorUTC.Add(steps * interval)
}

func (c *cronSchedule) nextAfter(after time.Time) (time.Time, error) {
	candidate := after.UTC().Truncate(time.Minute).Add(time.Minute)
	for {
		if candidate.Year() > 2199 {
			return time.Time{}, fmt.Errorf("no next execution time found")
		}
		if c.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
}

func (c *cronSchedule) matches(ts time.Time) bool {
	if !c.minutes.matches(ts.Minute()) || !c.hours.matches(ts.Hour()) || !c.months.matches(int(ts.Month())) || !c.years.matches(ts.Year()) {
		return false
	}

	if c.dom.noSpec {
		return c.dow.matches(ts)
	}
	return c.dom.matches(ts)
}

func (f simpleField) matches(v int) bool {
	if f.any {
		return true
	}
	_, ok := f.values[v]
	return ok
}

func (f dayOfMonthField) matches(ts time.Time) bool {
	if f.noSpec {
		return true
	}
	if f.any {
		return true
	}

	year := ts.Year()
	month := ts.Month()
	day := ts.Day()
	last := daysInMonth(year, month)

	if _, ok := f.values[day]; ok {
		return true
	}
	if f.lastDay && day == last {
		return true
	}
	if f.lastWeekday && day == lastWeekdayOfMonth(year, month) {
		return true
	}
	for _, base := range f.nearestWeekd {
		if day == nearestWeekday(year, month, base) {
			return true
		}
	}
	return false
}

func (f dayOfWeekField) matches(ts time.Time) bool {
	if f.noSpec {
		return true
	}
	if f.any {
		return true
	}

	weekday := awsWeekday(ts.Weekday())
	day := ts.Day()
	year := ts.Year()
	month := ts.Month()

	if _, ok := f.values[weekday]; ok {
		return true
	}
	for _, want := range f.lastWeekdays {
		if weekday == want && day == lastWeekdayOccurrence(year, month, want) {
			return true
		}
	}
	for _, nth := range f.nthWeekdays {
		if weekday == nth.weekday && day == nthWeekdayOccurrence(year, month, nth.weekday, nth.nth) {
			return true
		}
	}
	return false
}

func daysInMonth(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func awsWeekday(wd time.Weekday) int {
	if wd == time.Sunday {
		return 1
	}
	return int(wd) + 1
}

func lastWeekdayOfMonth(year int, month time.Month) int {
	for day := daysInMonth(year, month); day >= 1; day-- {
		wd := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday()
		if wd != time.Saturday && wd != time.Sunday {
			return day
		}
	}
	return 1
}

func nearestWeekday(year int, month time.Month, day int) int {
	last := daysInMonth(year, month)
	if day < 1 || day > last {
		return -1
	}
	wd := time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday()
	switch wd {
	case time.Saturday:
		if day == 1 {
			return 3
		}
		return day - 1
	case time.Sunday:
		if day == last {
			return day - 2
		}
		return day + 1
	default:
		return day
	}
}

func nthWeekdayOccurrence(year int, month time.Month, weekday, nth int) int {
	if nth < 1 || nth > 5 {
		return -1
	}
	count := 0
	for day := 1; day <= daysInMonth(year, month); day++ {
		wd := awsWeekday(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday())
		if wd != weekday {
			continue
		}
		count++
		if count == nth {
			return day
		}
	}
	return -1
}

func lastWeekdayOccurrence(year int, month time.Month, weekday int) int {
	for day := daysInMonth(year, month); day >= 1; day-- {
		wd := awsWeekday(time.Date(year, month, day, 0, 0, 0, 0, time.UTC).Weekday())
		if wd == weekday {
			return day
		}
	}
	return -1
}
