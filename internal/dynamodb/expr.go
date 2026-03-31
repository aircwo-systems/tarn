package dynamodb

import (
	"fmt"
	"math/big"
	"sort"
	"strconv"
	"strings"
)

type token struct {
	kind string
	text string
}

type exprParser struct {
	tokens []token
	pos    int
}

type exprNode interface {
	eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error)
}

type operandNode struct {
	raw     string
	isValue bool
}

type logicalNode struct {
	op          string
	left, right exprNode
}

type notNode struct {
	child exprNode
}

type compareNode struct {
	op          string
	left, right exprNode
}

type betweenNode struct {
	target exprNode
	low    exprNode
	high   exprNode
}

type inNode struct {
	target exprNode
	values []exprNode
}

type functionNode struct {
	name string
	args []exprNode
}

func evaluateConditionExpression(expr string, item map[string]any, names map[string]string, values map[string]any) (bool, error) {
	if strings.TrimSpace(expr) == "" {
		return true, nil
	}
	tokens, err := tokenizeExpression(expr)
	if err != nil {
		return false, err
	}
	root, err := (&exprParser{tokens: tokens}).parseExpression()
	if err != nil {
		return false, err
	}
	value, _, err := root.eval(item, names, values)
	if err != nil {
		return false, err
	}
	ok, _ := value.(bool)
	return ok, nil
}

func applyProjectionExpression(item map[string]any, expr string, names map[string]string) (map[string]any, error) {
	if strings.TrimSpace(expr) == "" || item == nil {
		return cloneItem(item), nil
	}
	parts := splitTopLevel(expr, ',')
	out := make(map[string]any)
	for _, part := range parts {
		path, err := resolvePath(strings.TrimSpace(part), names)
		if err != nil {
			return nil, err
		}
		value, ok := getPathValue(item, path)
		if !ok {
			continue
		}
		if err := assignPathValue(out, path, cloneAny(value)); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func applyUpdateExpression(item map[string]any, expr string, names map[string]string, values map[string]any) error {
	if strings.TrimSpace(expr) == "" {
		return validationError("UpdateExpression is required")
	}

	remaining := strings.TrimSpace(expr)
	for len(remaining) > 0 {
		keyword, body, rest, err := nextUpdateClause(remaining)
		if err != nil {
			return err
		}
		switch keyword {
		case "SET":
			if err := applySetClause(item, body, names, values); err != nil {
				return err
			}
		case "REMOVE":
			if err := applyRemoveClause(item, body, names); err != nil {
				return err
			}
		case "ADD":
			if err := applyAddClause(item, body, names, values); err != nil {
				return err
			}
		case "DELETE":
			if err := applyDeleteClause(item, body, names, values); err != nil {
				return err
			}
		default:
			return validationError("Unsupported update clause: %s", keyword)
		}
		remaining = strings.TrimSpace(rest)
	}
	return nil
}

func nextUpdateClause(expr string) (string, string, string, error) {
	expr = strings.TrimSpace(expr)
	for _, keyword := range []string{"SET", "REMOVE", "ADD", "DELETE"} {
		if !strings.HasPrefix(strings.ToUpper(expr), keyword) {
			continue
		}
		bodyStart := len(keyword)
		body := strings.TrimSpace(expr[bodyStart:])
		nextIdx := len(body)
		for _, nextKeyword := range []string{" SET ", " REMOVE ", " ADD ", " DELETE "} {
			if idx := indexClauseKeyword(body, nextKeyword); idx >= 0 && idx < nextIdx {
				nextIdx = idx
			}
		}
		return keyword, strings.TrimSpace(body[:nextIdx]), strings.TrimSpace(body[nextIdx:]), nil
	}
	return "", "", "", validationError("Invalid UpdateExpression")
}

func indexClauseKeyword(body, keyword string) int {
	depth := 0
	upper := strings.ToUpper(body)
	for i := 0; i+len(keyword) <= len(body); i++ {
		switch body[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(upper[i:], keyword) {
			return i
		}
	}
	return -1
}

func applySetClause(item map[string]any, body string, names map[string]string, values map[string]any) error {
	for _, part := range splitTopLevel(body, ',') {
		lhs, rhs, ok := strings.Cut(part, "=")
		if !ok {
			return validationError("Invalid SET assignment")
		}
		path, err := resolvePath(strings.TrimSpace(lhs), names)
		if err != nil {
			return err
		}
		value, err := evalUpdateValue(strings.TrimSpace(rhs), item, names, values)
		if err != nil {
			return err
		}
		if err := assignPathValue(item, path, value); err != nil {
			return err
		}
	}
	return nil
}

func evalUpdateValue(expr string, item map[string]any, names map[string]string, values map[string]any) (any, error) {
	if strings.HasPrefix(strings.ToLower(expr), "if_not_exists(") && strings.HasSuffix(expr, ")") {
		body := strings.TrimSuffix(strings.TrimPrefix(expr, "if_not_exists("), ")")
		parts := splitTopLevel(body, ',')
		if len(parts) != 2 {
			return nil, validationError("Invalid if_not_exists expression")
		}
		path, err := resolvePath(strings.TrimSpace(parts[0]), names)
		if err != nil {
			return nil, err
		}
		if value, ok := getPathValue(item, path); ok {
			return cloneAny(value), nil
		}
		return evalUpdateValue(strings.TrimSpace(parts[1]), item, names, values)
	}

	for _, op := range []string{"+", "-"} {
		if idx := topLevelOperatorIndex(expr, op); idx >= 0 {
			left, err := evalUpdateValue(strings.TrimSpace(expr[:idx]), item, names, values)
			if err != nil {
				return nil, err
			}
			right, err := evalUpdateValue(strings.TrimSpace(expr[idx+1:]), item, names, values)
			if err != nil {
				return nil, err
			}
			return arithmeticAttribute(left, right, op)
		}
	}

	if strings.HasPrefix(expr, ":") {
		value, ok := values[expr]
		if !ok {
			return nil, validationError("Missing expression value: %s", expr)
		}
		return cloneAny(value), nil
	}

	path, err := resolvePath(expr, names)
	if err != nil {
		return nil, err
	}
	value, ok := getPathValue(item, path)
	if !ok {
		return nil, nil
	}
	return cloneAny(value), nil
}

func applyRemoveClause(item map[string]any, body string, names map[string]string) error {
	for _, part := range splitTopLevel(body, ',') {
		path, err := resolvePath(strings.TrimSpace(part), names)
		if err != nil {
			return err
		}
		removePathValue(item, path)
	}
	return nil
}

func applyAddClause(item map[string]any, body string, names map[string]string, values map[string]any) error {
	for _, part := range splitTopLevel(body, ',') {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			return validationError("Invalid ADD expression")
		}
		path, err := resolvePath(fields[0], names)
		if err != nil {
			return err
		}
		existing, _ := getPathValue(item, path)
		value, ok := values[fields[1]]
		if !ok {
			return validationError("Missing expression value: %s", fields[1])
		}
		updated, err := addAttribute(existing, value)
		if err != nil {
			return err
		}
		if err := assignPathValue(item, path, updated); err != nil {
			return err
		}
	}
	return nil
}

func applyDeleteClause(item map[string]any, body string, names map[string]string, values map[string]any) error {
	for _, part := range splitTopLevel(body, ',') {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			return validationError("Invalid DELETE expression")
		}
		path, err := resolvePath(fields[0], names)
		if err != nil {
			return err
		}
		existing, _ := getPathValue(item, path)
		value, ok := values[fields[1]]
		if !ok {
			return validationError("Missing expression value: %s", fields[1])
		}
		updated, err := deleteFromSet(existing, value)
		if err != nil {
			return err
		}
		if updated == nil {
			removePathValue(item, path)
			continue
		}
		if err := assignPathValue(item, path, updated); err != nil {
			return err
		}
	}
	return nil
}

func arithmeticAttribute(left, right any, op string) (any, error) {
	ln, lok := asNumber(left)
	rn, rok := asNumber(right)
	if !lok || !rok {
		return nil, validationError("Arithmetic updates require numeric attribute values")
	}
	if op == "+" {
		ln = new(big.Float).Add(ln, rn)
	} else {
		ln = new(big.Float).Sub(ln, rn)
	}
	text := strings.TrimRight(strings.TrimRight(ln.Text('f', -1), "0"), ".")
	if text == "" || text == "-" {
		text = "0"
	}
	return map[string]any{"N": text}, nil
}

func addAttribute(existing, incoming any) (any, error) {
	if existing == nil {
		return cloneAny(incoming), nil
	}
	if _, ok := asNumber(existing); ok {
		return arithmeticAttribute(existing, incoming, "+")
	}
	existingSet, existingType, err := setValues(existing)
	if err != nil {
		return nil, err
	}
	incomingSet, incomingType, err := setValues(incoming)
	if err != nil {
		return nil, err
	}
	if existingType != incomingType {
		return nil, validationError("ADD on sets requires matching types")
	}
	seen := make(map[string]any, len(existingSet)+len(incomingSet))
	for _, entry := range append(existingSet, incomingSet...) {
		key := fmt.Sprint(entry)
		seen[key] = entry
	}
	keys := make([]string, 0, len(seen))
	for key := range seen {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, seen[key])
	}
	return map[string]any{existingType: out}, nil
}

func deleteFromSet(existing, incoming any) (any, error) {
	existingSet, existingType, err := setValues(existing)
	if err != nil {
		return nil, err
	}
	incomingSet, incomingType, err := setValues(incoming)
	if err != nil {
		return nil, err
	}
	if existingType != incomingType {
		return nil, validationError("DELETE on sets requires matching types")
	}
	remove := make(map[string]struct{}, len(incomingSet))
	for _, entry := range incomingSet {
		remove[fmt.Sprint(entry)] = struct{}{}
	}
	out := make([]any, 0, len(existingSet))
	for _, entry := range existingSet {
		if _, ok := remove[fmt.Sprint(entry)]; !ok {
			out = append(out, entry)
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return map[string]any{existingType: out}, nil
}

func setValues(value any) ([]any, string, error) {
	attr, ok := value.(map[string]any)
	if !ok || len(attr) != 1 {
		return nil, "", validationError("Operation requires a set attribute")
	}
	for key, raw := range attr {
		switch key {
		case "SS", "NS", "BS":
			list, ok := raw.([]any)
			if !ok {
				if strings, ok := raw.([]string); ok {
					list = make([]any, 0, len(strings))
					for _, item := range strings {
						list = append(list, item)
					}
				}
			}
			if list == nil {
				return nil, "", validationError("Set attribute is invalid")
			}
			return list, key, nil
		}
	}
	return nil, "", validationError("Operation requires a set attribute")
}

func topLevelOperatorIndex(expr, op string) int {
	depth := 0
	for i := 0; i < len(expr); i++ {
		switch expr[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if depth == 0 && strings.HasPrefix(expr[i:], op) {
			return i
		}
	}
	return -1
}

func tokenizeExpression(expr string) ([]token, error) {
	tokens := make([]token, 0)
	for i := 0; i < len(expr); {
		ch := expr[i]
		switch {
		case ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r':
			i++
		case strings.ContainsRune("(),", rune(ch)):
			tokens = append(tokens, token{kind: string(ch), text: string(ch)})
			i++
		case i+1 < len(expr) && (expr[i:i+2] == "<=" || expr[i:i+2] == ">=" || expr[i:i+2] == "<>"):
			tokens = append(tokens, token{kind: "op", text: expr[i : i+2]})
			i += 2
		case strings.ContainsRune("=<>", rune(ch)):
			tokens = append(tokens, token{kind: "op", text: string(ch)})
			i++
		default:
			start := i
			for i < len(expr) && isExprChar(expr[i]) {
				i++
			}
			word := expr[start:i]
			upper := strings.ToUpper(word)
			switch upper {
			case "AND", "OR", "NOT", "BETWEEN", "IN":
				tokens = append(tokens, token{kind: upper, text: upper})
			default:
				tokens = append(tokens, token{kind: "word", text: word})
			}
		}
	}
	return tokens, nil
}

func isExprChar(ch byte) bool {
	return ch == '#' || ch == ':' || ch == '_' || ch == '.' || ch == '[' || ch == ']' || ch == '-' ||
		(ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9')
}

func (p *exprParser) parseExpression() (exprNode, error) {
	return p.parseOr()
}

func (p *exprParser) parseOr() (exprNode, error) {
	left, err := p.parseAnd()
	if err != nil {
		return nil, err
	}
	for p.match("OR") {
		right, err := p.parseAnd()
		if err != nil {
			return nil, err
		}
		left = logicalNode{op: "OR", left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseAnd() (exprNode, error) {
	left, err := p.parseNot()
	if err != nil {
		return nil, err
	}
	for p.match("AND") {
		right, err := p.parseNot()
		if err != nil {
			return nil, err
		}
		left = logicalNode{op: "AND", left: left, right: right}
	}
	return left, nil
}

func (p *exprParser) parseNot() (exprNode, error) {
	if p.match("NOT") {
		child, err := p.parsePrimary()
		if err != nil {
			return nil, err
		}
		return notNode{child: child}, nil
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (exprNode, error) {
	if p.match("(") {
		node, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if !p.match(")") {
			return nil, validationError("Invalid expression")
		}
		return node, nil
	}

	left, err := p.parseOperand()
	if err != nil {
		return nil, err
	}
	if p.peek("(") {
		if operand, ok := left.(operandNode); ok {
			return p.parseFunction(operand.raw)
		}
	}
	if p.match("BETWEEN") {
		low, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		if !p.match("AND") {
			return nil, validationError("Invalid BETWEEN expression")
		}
		high, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return betweenNode{target: left, low: low, high: high}, nil
	}
	if p.match("IN") {
		if !p.match("(") {
			return nil, validationError("Invalid IN expression")
		}
		values := make([]exprNode, 0)
		for {
			operand, err := p.parseOperand()
			if err != nil {
				return nil, err
			}
			values = append(values, operand)
			if p.match(")") {
				break
			}
			if !p.match(",") {
				return nil, validationError("Invalid IN expression")
			}
		}
		return inNode{target: left, values: values}, nil
	}
	if p.peek("op") {
		op := p.consume().text
		right, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		return compareNode{op: op, left: left, right: right}, nil
	}
	return left, nil
}

func (p *exprParser) parseFunction(name string) (exprNode, error) {
	if !p.match("(") {
		return nil, validationError("Invalid function call")
	}
	args := make([]exprNode, 0)
	if p.match(")") {
		return functionNode{name: name, args: args}, nil
	}
	for {
		arg, err := p.parseOperand()
		if err != nil {
			return nil, err
		}
		args = append(args, arg)
		if p.match(")") {
			break
		}
		if !p.match(",") {
			return nil, validationError("Invalid function call")
		}
	}
	return functionNode{name: name, args: args}, nil
}

func (p *exprParser) parseOperand() (exprNode, error) {
	if p.pos >= len(p.tokens) {
		return nil, validationError("Unexpected end of expression")
	}
	tok := p.consume()
	if tok.kind != "word" {
		return nil, validationError("Invalid expression token: %s", tok.text)
	}
	return operandNode{raw: tok.text, isValue: strings.HasPrefix(tok.text, ":")}, nil
}

func (p *exprParser) match(kind string) bool {
	if p.peek(kind) {
		p.pos++
		return true
	}
	return false
}

func (p *exprParser) peek(kind string) bool {
	if p.pos >= len(p.tokens) {
		return false
	}
	tok := p.tokens[p.pos]
	return tok.kind == kind || tok.text == kind
}

func (p *exprParser) consume() token {
	tok := p.tokens[p.pos]
	p.pos++
	return tok
}

func (n operandNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	if n.isValue {
		value, ok := values[n.raw]
		if !ok {
			return nil, false, validationError("Missing expression value: %s", n.raw)
		}
		return cloneAny(value), true, nil
	}
	path, err := resolvePath(n.raw, names)
	if err != nil {
		return nil, false, err
	}
	value, ok := getPathValue(item, path)
	return value, ok, nil
}

func (n logicalNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	left, _, err := n.left.eval(item, names, values)
	if err != nil {
		return nil, false, err
	}
	leftBool, _ := left.(bool)
	if n.op == "AND" && !leftBool {
		return false, true, nil
	}
	if n.op == "OR" && leftBool {
		return true, true, nil
	}
	right, _, err := n.right.eval(item, names, values)
	if err != nil {
		return nil, false, err
	}
	rightBool, _ := right.(bool)
	if n.op == "AND" {
		return leftBool && rightBool, true, nil
	}
	return leftBool || rightBool, true, nil
}

func (n notNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	value, _, err := n.child.eval(item, names, values)
	if err != nil {
		return nil, false, err
	}
	b, _ := value.(bool)
	return !b, true, nil
}

func (n compareNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	left, leftExists, err := n.left.eval(item, names, values)
	if err != nil {
		return nil, false, err
	}
	right, rightExists, err := n.right.eval(item, names, values)
	if err != nil {
		return nil, false, err
	}
	if !leftExists || !rightExists {
		return false, true, nil
	}
	cmp := compareAttributeValues(left, right)
	switch n.op {
	case "=":
		return cmp == 0, true, nil
	case "<>":
		return cmp != 0, true, nil
	case "<":
		return cmp < 0, true, nil
	case "<=":
		return cmp <= 0, true, nil
	case ">":
		return cmp > 0, true, nil
	case ">=":
		return cmp >= 0, true, nil
	default:
		return nil, false, validationError("Unsupported comparator: %s", n.op)
	}
}

func (n betweenNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	target, ok, err := n.target.eval(item, names, values)
	if err != nil || !ok {
		return false, true, err
	}
	low, ok, err := n.low.eval(item, names, values)
	if err != nil || !ok {
		return false, true, err
	}
	high, ok, err := n.high.eval(item, names, values)
	if err != nil || !ok {
		return false, true, err
	}
	return compareAttributeValues(target, low) >= 0 && compareAttributeValues(target, high) <= 0, true, nil
}

func (n inNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	target, ok, err := n.target.eval(item, names, values)
	if err != nil || !ok {
		return false, true, err
	}
	for _, candidateNode := range n.values {
		candidate, ok, err := candidateNode.eval(item, names, values)
		if err != nil {
			return nil, false, err
		}
		if ok && attributeValueEqual(target, candidate) {
			return true, true, nil
		}
	}
	return false, true, nil
}

func (n functionNode) eval(item map[string]any, names map[string]string, values map[string]any) (any, bool, error) {
	switch strings.ToLower(n.name) {
	case "attribute_exists":
		if len(n.args) != 1 {
			return nil, false, validationError("attribute_exists expects one argument")
		}
		_, ok, err := n.args[0].eval(item, names, values)
		return ok, true, err
	case "attribute_not_exists":
		if len(n.args) != 1 {
			return nil, false, validationError("attribute_not_exists expects one argument")
		}
		_, ok, err := n.args[0].eval(item, names, values)
		return !ok, true, err
	case "begins_with":
		if len(n.args) != 2 {
			return nil, false, validationError("begins_with expects two arguments")
		}
		left, leftOK, err := n.args[0].eval(item, names, values)
		if err != nil || !leftOK {
			return false, true, err
		}
		right, rightOK, err := n.args[1].eval(item, names, values)
		if err != nil || !rightOK {
			return false, true, err
		}
		return strings.HasPrefix(attributeScalarString(left), attributeScalarString(right)), true, nil
	default:
		return nil, false, validationError("Unsupported function: %s", n.name)
	}
}

func resolvePath(raw string, names map[string]string) ([]pathSegment, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, validationError("Invalid path")
	}
	parts := strings.Split(raw, ".")
	segments := make([]pathSegment, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, validationError("Invalid path")
		}
		base := part
		suffixes := make([]int, 0)
		for {
			start := strings.Index(base, "[")
			if start < 0 {
				break
			}
			end := strings.Index(base[start:], "]")
			if end < 0 {
				return nil, validationError("Invalid list index in path")
			}
			indexValue, err := strconv.Atoi(base[start+1 : start+end])
			if err != nil {
				return nil, validationError("Invalid list index in path")
			}
			suffixes = append(suffixes, indexValue)
			base = base[:start] + base[start+end+1:]
		}
		if replacement, ok := names[base]; ok {
			base = replacement
		}
		segments = append(segments, pathSegment{name: base})
		for _, idx := range suffixes {
			segments = append(segments, pathSegment{index: idx, isIndex: true})
		}
	}
	return segments, nil
}

type pathSegment struct {
	name    string
	index   int
	isIndex bool
}

func getPathValue(item map[string]any, path []pathSegment) (any, bool) {
	if len(path) == 0 {
		return nil, false
	}
	var current any = item
	for i, segment := range path {
		if !segment.isIndex {
			if i == 0 {
				currentMap, ok := current.(map[string]any)
				if !ok {
					return nil, false
				}
				value, ok := currentMap[segment.name]
				if !ok {
					return nil, false
				}
				current = value
				continue
			}
			attr, ok := current.(map[string]any)
			if !ok {
				return nil, false
			}
			if nested, ok := attr["M"].(map[string]any); ok {
				value, ok := nested[segment.name]
				if !ok {
					return nil, false
				}
				current = value
				continue
			}
			return nil, false
		}
		attr, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		list, ok := attr["L"].([]any)
		if !ok || segment.index < 0 || segment.index >= len(list) {
			return nil, false
		}
		current = list[segment.index]
	}
	return current, true
}

func assignPathValue(item map[string]any, path []pathSegment, value any) error {
	if len(path) == 0 {
		return validationError("Invalid path")
	}
	if len(path) == 1 && !path[0].isIndex {
		item[path[0].name] = cloneAny(value)
		return nil
	}
	var current any = item
	for i, segment := range path[:len(path)-1] {
		next := path[i+1]
		if !segment.isIndex {
			if i == 0 {
				root := current.(map[string]any)
				child, ok := root[segment.name]
				if !ok || child == nil {
					if next.isIndex {
						return validationError("Cannot create list path automatically")
					}
					child = map[string]any{"M": map[string]any{}}
					root[segment.name] = child
				}
				current = child
				continue
			}
			attr, ok := current.(map[string]any)
			if !ok {
				return validationError("Invalid document path")
			}
			nested, ok := attr["M"].(map[string]any)
			if !ok {
				return validationError("Invalid document path")
			}
			child, ok := nested[segment.name]
			if !ok || child == nil {
				if next.isIndex {
					return validationError("Cannot create list path automatically")
				}
				child = map[string]any{"M": map[string]any{}}
				nested[segment.name] = child
			}
			current = child
			continue
		}
		attr, ok := current.(map[string]any)
		if !ok {
			return validationError("Invalid document path")
		}
		list, ok := attr["L"].([]any)
		if !ok || segment.index < 0 || segment.index >= len(list) {
			return validationError("Invalid document path")
		}
		current = list[segment.index]
	}

	last := path[len(path)-1]
	if last.isIndex {
		attr, ok := current.(map[string]any)
		if !ok {
			return validationError("Invalid document path")
		}
		list, ok := attr["L"].([]any)
		if !ok || last.index < 0 || last.index >= len(list) {
			return validationError("Invalid document path")
		}
		list[last.index] = cloneAny(value)
		attr["L"] = list
		return nil
	}
	if currentMap, ok := current.(map[string]any); ok {
		if nested, ok := currentMap["M"].(map[string]any); ok {
			nested[last.name] = cloneAny(value)
			currentMap["M"] = nested
			return nil
		}
	}
	if root, ok := current.(map[string]any); ok {
		root[last.name] = cloneAny(value)
		return nil
	}
	return validationError("Invalid document path")
}

func removePathValue(item map[string]any, path []pathSegment) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 && !path[0].isIndex {
		delete(item, path[0].name)
		return
	}
	var current any = item
	for i, segment := range path[:len(path)-1] {
		if !segment.isIndex {
			if i == 0 {
				root := current.(map[string]any)
				current = root[segment.name]
				continue
			}
			attr, ok := current.(map[string]any)
			if !ok {
				return
			}
			nested, ok := attr["M"].(map[string]any)
			if !ok {
				return
			}
			current = nested[segment.name]
			continue
		}
		attr, ok := current.(map[string]any)
		if !ok {
			return
		}
		list, ok := attr["L"].([]any)
		if !ok || segment.index < 0 || segment.index >= len(list) {
			return
		}
		current = list[segment.index]
	}
	last := path[len(path)-1]
	if last.isIndex {
		attr, ok := current.(map[string]any)
		if !ok {
			return
		}
		list, ok := attr["L"].([]any)
		if !ok || last.index < 0 || last.index >= len(list) {
			return
		}
		list = append(list[:last.index], list[last.index+1:]...)
		attr["L"] = list
		return
	}
	if attr, ok := current.(map[string]any); ok {
		if nested, ok := attr["M"].(map[string]any); ok {
			delete(nested, last.name)
			attr["M"] = nested
			return
		}
		delete(attr, last.name)
	}
}

func attributeScalarString(value any) string {
	attr, ok := value.(map[string]any)
	if !ok || len(attr) != 1 {
		return fmt.Sprint(value)
	}
	switch {
	case attr["S"] != nil:
		return fmt.Sprint(attr["S"])
	case attr["N"] != nil:
		return fmt.Sprint(attr["N"])
	case attr["B"] != nil:
		return fmt.Sprint(attr["B"])
	default:
		return fmt.Sprint(value)
	}
}

func splitTopLevel(input string, sep rune) []string {
	depth := 0
	current := strings.Builder{}
	out := make([]string, 0)
	for _, ch := range input {
		switch ch {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		}
		if ch == sep && depth == 0 {
			out = append(out, strings.TrimSpace(current.String()))
			current.Reset()
			continue
		}
		current.WriteRune(ch)
	}
	if current.Len() > 0 {
		out = append(out, strings.TrimSpace(current.String()))
	}
	return out
}
