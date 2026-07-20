package schematic

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type tokenKind uint8

const (
	tokEOF tokenKind = iota
	tokNL
	tokIdent
	tokNumber
	tokHex
	tokLParen
	tokRParen
	tokLBrace
	tokRBrace
	tokEqual
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokPercent
	tokRange
	tokEqEq
	tokNotEq
	tokLT
	tokLTE
	tokGT
	tokGTE
	tokAndAnd
	tokOrOr
	tokBang
)

type token struct {
	kind      tokenKind
	text      string
	line, col int
}

type DiagnosticError struct {
	Line, Column int
	Message      string
	LineText     string
}

func (e *DiagnosticError) Error() string {
	if e.Line <= 0 {
		return e.Message
	}
	marker := strings.Repeat(" ", max(e.Column-1, 0)) + "^"
	return fmt.Sprintf("line %d, column %d: %s\n%s\n%s", e.Line, e.Column, e.Message, e.LineText, marker)
}

func lex(source string) ([]token, error) {
	if len(source) > MaxSourceBytes {
		return nil, fmt.Errorf("source is %d bytes; maximum is %d", len(source), MaxSourceBytes)
	}
	var out []token
	line, col := 1, 1
	for i := 0; i < len(source); {
		c := source[i]
		if c == '\r' {
			i++
			continue
		}
		if c == '\n' || c == ';' {
			out = append(out, token{kind: tokNL, text: string(c), line: line, col: col})
			i++
			if c == '\n' {
				line++
				col = 1
			} else {
				col++
			}
			continue
		}
		if c == ' ' || c == '\t' || c == ',' {
			i++
			col++
			continue
		}
		if c == '#' {
			start, startCol := i, col
			i++
			col++
			for i < len(source) && isHex(source[i]) && i-start <= 6 {
				i++
				col++
			}
			if i-start == 7 && (i == len(source) || !isIdentPart(source[i])) {
				out = append(out, token{kind: tokHex, text: source[start:i], line: line, col: startCol})
				continue
			}
			for i < len(source) && source[i] != '\n' {
				i++
				col++
			}
			continue
		}
		start, startCol := i, col
		if c >= '0' && c <= '9' {
			for i < len(source) && source[i] >= '0' && source[i] <= '9' {
				i++
				col++
			}
			out = append(out, token{kind: tokNumber, text: source[start:i], line: line, col: startCol})
			continue
		}
		if isIdentStart(c) {
			for i < len(source) && isIdentPart(source[i]) {
				// A single dot is valid in names such as namespaced registry IDs,
				// but two dots always begin an inclusive loop range.
				if source[i] == '.' && i+1 < len(source) && source[i+1] == '.' {
					break
				}
				i++
				col++
			}
			out = append(out, token{kind: tokIdent, text: source[start:i], line: line, col: startCol})
			continue
		}
		kind := tokEOF
		switch c {
		case '(':
			kind = tokLParen
		case ')':
			kind = tokRParen
		case '{':
			kind = tokLBrace
		case '}':
			kind = tokRBrace
		case '=':
			if i+1 < len(source) && source[i+1] == '=' {
				kind = tokEqEq
				i++
				col++
			} else {
				kind = tokEqual
			}
		case '!':
			kind = tokBang
			if i+1 < len(source) && source[i+1] == '=' {
				kind = tokNotEq
				i++
				col++
			}
		case '<':
			kind = tokLT
			if i+1 < len(source) && source[i+1] == '=' {
				kind = tokLTE
				i++
				col++
			}
		case '>':
			kind = tokGT
			if i+1 < len(source) && source[i+1] == '=' {
				kind = tokGTE
				i++
				col++
			}
		case '&':
			if i+1 < len(source) && source[i+1] == '&' {
				kind = tokAndAnd
				i++
				col++
			}
		case '|':
			if i+1 < len(source) && source[i+1] == '|' {
				kind = tokOrOr
				i++
				col++
			}
		case '+':
			kind = tokPlus
		case '-':
			kind = tokMinus
		case '*':
			kind = tokStar
		case '/':
			kind = tokSlash
		case '%':
			kind = tokPercent
		case '.':
			if i+1 < len(source) && source[i+1] == '.' {
				kind = tokRange
				i++
				col++
			}
		}
		if kind == tokEOF {
			return nil, diagnostic(source, token{line: line, col: col}, fmt.Sprintf("unexpected character %q", c))
		}
		out = append(out, token{kind: kind, text: source[start : i+1], line: line, col: startCol})
		i++
		col++
	}
	out = append(out, token{kind: tokEOF, line: line, col: col})
	return out, nil
}

func isHex(c byte) bool        { return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
func isIdentStart(c byte) bool { return c == '_' || unicode.IsLetter(rune(c)) }
func isIdentPart(c byte) bool {
	return isIdentStart(c) || c >= '0' && c <= '9' || c == ':' || c == '-' || c == '.'
}

func diagnostic(source string, t token, message string) error {
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	lineText := ""
	if t.line > 0 && t.line <= len(lines) {
		lineText = lines[t.line-1]
	}
	return &DiagnosticError{Line: t.line, Column: t.col, Message: message, LineText: lineText}
}

type expr struct {
	kind        tokenKind
	value       int64
	name        string
	left, right *expr
	pos         token
}

func (e *expr) eval(vars map[string]int64) (int64, error) {
	if e == nil {
		return 0, fmt.Errorf("missing expression")
	}
	switch e.kind {
	case tokNumber:
		return e.value, nil
	case tokIdent:
		v, ok := vars[e.name]
		if !ok {
			return 0, fmt.Errorf("unknown variable %q", e.name)
		}
		return v, nil
	case tokBang:
		a, err := e.left.eval(vars)
		if err != nil {
			return 0, err
		}
		if a == 0 {
			return 1, nil
		}
		return 0, nil
	case tokAndAnd:
		a, err := e.left.eval(vars)
		if err != nil || a == 0 {
			return 0, err
		}
		b, err := e.right.eval(vars)
		if err != nil || b == 0 {
			return 0, err
		}
		return 1, nil
	case tokOrOr:
		a, err := e.left.eval(vars)
		if err != nil {
			return 0, err
		}
		if a != 0 {
			return 1, nil
		}
		b, err := e.right.eval(vars)
		if err != nil || b == 0 {
			return 0, err
		}
		return 1, nil
	case tokPlus, tokMinus, tokStar, tokSlash, tokPercent, tokEqEq, tokNotEq, tokLT, tokLTE, tokGT, tokGTE:
		a, err := e.left.eval(vars)
		if err != nil {
			return 0, err
		}
		if e.right == nil {
			if e.kind == tokMinus && a == math.MinInt64 {
				return 0, fmt.Errorf("integer overflow")
			}
			if e.kind == tokMinus {
				return -a, nil
			}
			return a, nil
		}
		b, err := e.right.eval(vars)
		if err != nil {
			return 0, err
		}
		switch e.kind {
		case tokPlus:
			v := a + b
			if (b > 0 && v < a) || (b < 0 && v > a) {
				return 0, fmt.Errorf("integer overflow")
			}
			return v, nil
		case tokMinus:
			if b == math.MinInt64 {
				return 0, fmt.Errorf("integer overflow")
			}
			return (&expr{kind: tokPlus, left: &expr{kind: tokNumber, value: a}, right: &expr{kind: tokNumber, value: -b}}).eval(vars)
		case tokStar:
			if a != 0 && (a == math.MinInt64 && b == -1 || b == math.MinInt64 && a == -1 || a*b/a != b) {
				return 0, fmt.Errorf("integer overflow")
			}
			return a * b, nil
		case tokSlash:
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			if a == math.MinInt64 && b == -1 {
				return 0, fmt.Errorf("integer overflow")
			}
			return a / b, nil
		case tokPercent:
			if b == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			return a % b, nil
		case tokEqEq:
			return boolInt(a == b), nil
		case tokNotEq:
			return boolInt(a != b), nil
		case tokLT:
			return boolInt(a < b), nil
		case tokLTE:
			return boolInt(a <= b), nil
		case tokGT:
			return boolInt(a > b), nil
		case tokGTE:
			return boolInt(a >= b), nil
		}
	}
	return 0, fmt.Errorf("invalid expression")
}

type statement struct {
	op             string
	pos            token
	name, material string
	params         []string
	args           []*expr
	body           []statement
	elseBody       []statement
}

type parser struct {
	source   string
	tokens   []token
	i, depth int
}

func Parse(source string) ([]statement, string, error) {
	source = extractProgram(source)
	tokens, err := lex(source)
	if err != nil {
		return nil, source, err
	}
	p := &parser{source: source, tokens: tokens}
	program, err := p.statements(false)
	return program, source, err
}

func extractProgram(source string) string {
	source = strings.TrimSpace(source)
	if i := strings.Index(source, "```"); i >= 0 {
		rest := source[i+3:]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			head := strings.TrimSpace(rest[:nl])
			if head == "" || strings.EqualFold(head, "vxl") || strings.EqualFold(head, "voxel") || strings.EqualFold(head, "schematic") {
				rest = rest[nl+1:]
			}
		}
		if end := strings.Index(rest, "```"); end >= 0 {
			return strings.TrimSpace(rest[:end])
		}
	}
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) > 0 && isStatement(fields[0]) {
			return strings.TrimSpace(strings.Join(lines[i:], "\n"))
		}
	}
	return source
}

func isStatement(s string) bool {
	switch strings.ToLower(s) {
	case "mat", "let", "template", "paste", "for", "if", "at", "b", "block", "ln", "line", "box", "hbox", "sph", "sphere", "hsph", "ell", "hell", "cyl", "tube":
		return true
	default:
		return false
	}
}

func (p *parser) peek() token { return p.tokens[min(p.i, len(p.tokens)-1)] }
func (p *parser) take() token {
	t := p.peek()
	if p.i < len(p.tokens) {
		p.i++
	}
	return t
}
func (p *parser) skipNL() {
	for p.peek().kind == tokNL {
		p.take()
	}
}
func (p *parser) fail(t token, msg string) error { return diagnostic(p.source, t, msg) }

func (p *parser) statements(untilBrace bool) ([]statement, error) {
	var out []statement
	for {
		p.skipNL()
		t := p.peek()
		if t.kind == tokEOF {
			if untilBrace {
				return nil, p.fail(t, "missing closing }")
			}
			return out, nil
		}
		if t.kind == tokRBrace {
			if !untilBrace {
				return nil, p.fail(t, "unexpected }")
			}
			p.take()
			return out, nil
		}
		s, err := p.statement()
		if err != nil {
			return nil, err
		}
		out = append(out, s)
		if p.peek().kind != tokNL && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
			return nil, p.fail(p.peek(), "expected end of statement")
		}
	}
}

func (p *parser) statement() (statement, error) {
	t := p.take()
	if t.kind != tokIdent {
		return statement{}, p.fail(t, "expected statement")
	}
	op := strings.ToLower(t.text)
	s := statement{op: op, pos: t}
	switch op {
	case "mat":
		n := p.take()
		if n.kind != tokIdent {
			return s, p.fail(n, "expected material alias")
		}
		s.name = n.text
		if p.peek().kind == tokEqual {
			p.take()
		}
		v := p.take()
		if v.kind != tokIdent && v.kind != tokHex {
			return s, p.fail(v, "expected a block ID or #RRGGBB color")
		}
		s.material = v.text
		return s, nil
	case "let":
		n := p.take()
		if n.kind != tokIdent {
			return s, p.fail(n, "expected variable name")
		}
		s.name = n.text
		if p.peek().kind == tokEqual {
			p.take()
		}
		// Be lenient when a model uses generic assignment syntax for an
		// unmistakable RGB material. Integer lets retain their normal meaning.
		if p.peek().kind == tokHex {
			s.op = "mat"
			s.material = p.take().text
			return s, nil
		}
		e, err := p.expression(0)
		s.args = []*expr{e}
		return s, err
	case "template":
		if p.depth >= MaxNestingDepth {
			return s, p.fail(t, fmt.Sprintf("nesting exceeds %d", MaxNestingDepth))
		}
		n := p.take()
		if n.kind != tokIdent {
			return s, p.fail(n, "expected template name")
		}
		s.name = n.text
		if p.peek().kind != tokLParen {
			return s, p.fail(p.peek(), "expected ( after template name")
		}
		p.take()
		seen := map[string]bool{}
		for p.peek().kind != tokRParen {
			param := p.take()
			if param.kind != tokIdent {
				return s, p.fail(param, "expected parameter name or )")
			}
			if seen[param.text] {
				return s, p.fail(param, "duplicate template parameter "+param.text)
			}
			seen[param.text] = true
			s.params = append(s.params, param.text)
		}
		p.take()
		p.skipNL()
		if p.peek().kind != tokLBrace {
			return s, p.fail(p.peek(), "expected { after template parameters")
		}
		p.take()
		p.depth++
		body, err := p.statements(true)
		s.body = body
		p.depth--
		return s, err
	case "paste":
		n := p.take()
		if n.kind != tokIdent {
			return s, p.fail(n, "expected template name")
		}
		s.name = n.text
		for p.peek().kind != tokNL && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
			e, err := p.expression(0)
			if err != nil {
				return s, err
			}
			s.args = append(s.args, e)
		}
		return s, nil
	case "for":
		if p.depth >= MaxNestingDepth {
			return s, p.fail(t, fmt.Sprintf("nesting exceeds %d", MaxNestingDepth))
		}
		n := p.take()
		if n.kind != tokIdent {
			return s, p.fail(n, "expected loop variable")
		}
		s.name = n.text
		if p.peek().kind == tokEqual {
			p.take()
		}
		a, err := p.expression(0)
		if err != nil {
			return s, err
		}
		if p.peek().kind != tokRange {
			return s, p.fail(p.peek(), "expected .. in loop range")
		}
		p.take()
		b, err := p.expression(0)
		if err != nil {
			return s, err
		}
		s.args = []*expr{a, b}
		if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "step") {
			p.take()
			e, err := p.expression(0)
			if err != nil {
				return s, err
			}
			s.args = append(s.args, e)
		}
		p.skipNL()
		if p.peek().kind != tokLBrace {
			return s, p.fail(p.peek(), "expected { after loop")
		}
		p.take()
		p.depth++
		s.body, err = p.statements(true)
		p.depth--
		return s, err
	case "if":
		if p.depth >= MaxNestingDepth {
			return s, p.fail(t, fmt.Sprintf("nesting exceeds %d", MaxNestingDepth))
		}
		condition, err := p.expression(0)
		if err != nil {
			return s, err
		}
		s.args = []*expr{condition}
		p.skipNL()
		if p.peek().kind != tokLBrace {
			return s, p.fail(p.peek(), "expected { after condition")
		}
		p.take()
		p.depth++
		s.body, err = p.statements(true)
		p.depth--
		if err != nil {
			return s, err
		}
		beforeElse := p.i
		p.skipNL()
		if p.peek().kind == tokIdent && strings.EqualFold(p.peek().text, "else") {
			p.take()
			p.skipNL()
			if p.peek().kind != tokLBrace {
				return s, p.fail(p.peek(), "expected { after else")
			}
			p.take()
			p.depth++
			s.elseBody, err = p.statements(true)
			p.depth--
		} else {
			p.i = beforeElse
		}
		return s, err
	case "at":
		if p.depth >= MaxNestingDepth {
			return s, p.fail(t, fmt.Sprintf("nesting exceeds %d", MaxNestingDepth))
		}
		args, err := p.expressions(3)
		if err != nil {
			return s, err
		}
		s.args = args
		p.skipNL()
		if p.peek().kind != tokLBrace {
			return s, p.fail(p.peek(), "expected { after translation")
		}
		p.take()
		p.depth++
		s.body, err = p.statements(true)
		p.depth--
		return s, err
	}
	counts := map[string]int{"b": 3, "block": 3, "ln": 6, "line": 6, "box": 6, "hbox": 6, "sph": 4, "sphere": 4, "hsph": 4, "ell": 6, "hell": 6, "cyl": 7, "tube": 7}
	n, ok := counts[op]
	if !ok {
		return s, p.fail(t, "unknown statement "+t.text)
	}
	m := p.take()
	if m.kind != tokIdent {
		return s, p.fail(m, "expected material alias")
	}
	s.material = m.text
	args, err := p.expressions(n)
	if err != nil {
		return s, err
	}
	s.args = args
	if p.peek().kind != tokNL && p.peek().kind != tokRBrace && p.peek().kind != tokEOF {
		e, err := p.expression(0)
		if err != nil {
			return s, err
		}
		s.args = append(s.args, e)
	}
	return s, nil
}

func (p *parser) expressions(n int) ([]*expr, error) {
	out := make([]*expr, 0, n)
	for range n {
		e, err := p.expression(0)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}
func precedence(k tokenKind) int {
	switch k {
	case tokOrOr:
		return 1
	case tokAndAnd:
		return 2
	case tokEqEq, tokNotEq, tokLT, tokLTE, tokGT, tokGTE:
		return 3
	case tokPlus, tokMinus:
		return 4
	case tokStar, tokSlash, tokPercent:
		return 5
	}
	return 0
}
func (p *parser) expression(minPrec int) (*expr, error) {
	t := p.take()
	var left *expr
	switch t.kind {
	case tokNumber:
		v, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return nil, p.fail(t, "invalid integer")
		}
		left = &expr{kind: t.kind, value: v, pos: t}
	case tokIdent:
		left = &expr{kind: t.kind, name: t.text, pos: t}
	case tokPlus, tokMinus, tokBang:
		e, err := p.expression(6)
		if err != nil {
			return nil, err
		}
		left = &expr{kind: t.kind, left: e, pos: t}
	case tokLParen:
		e, err := p.expression(0)
		if err != nil {
			return nil, err
		}
		if p.peek().kind != tokRParen {
			return nil, p.fail(p.peek(), "expected )")
		}
		p.take()
		left = e
	default:
		return nil, p.fail(t, "expected integer expression")
	}
	for {
		op := p.peek()
		prec := precedence(op.kind)
		if prec == 0 || prec < minPrec {
			break
		}
		p.take()
		right, err := p.expression(prec + 1)
		if err != nil {
			return nil, err
		}
		left = &expr{kind: op.kind, left: left, right: right, pos: op}
	}
	return left, nil
}

type Point struct{ X, Y, Z int }
type Build struct {
	Bounds                    Bounds
	Blocks                    map[Point]string
	Materials                 map[string]ResolvedMaterial
	Writes, Primitives, Loops int
}

type executor struct {
	ctx            context.Context
	build          *Build
	vars           map[string]int64
	templates      map[string]statement
	templateDepth  int
	templatePastes int
	offset         Point
}

func Execute(ctx context.Context, program []statement, bounds Bounds, catalog *Catalog) (*Build, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	e := &executor{ctx: ctx, build: &Build{Bounds: bounds, Blocks: make(map[Point]string), Materials: map[string]ResolvedMaterial{"air": {ID: "minecraft:air", Legacy: "minecraft:air"}}}, vars: map[string]int64{}, templates: map[string]statement{}}
	if err := e.run(program, catalog); err != nil {
		return nil, err
	}
	if len(e.build.Blocks) == 0 {
		return nil, ErrNoBlocks
	}
	return e.build, nil
}

func (e *executor) run(stmts []statement, catalog *Catalog) error {
	for _, s := range stmts {
		if err := e.ctx.Err(); err != nil {
			return err
		}
		switch s.op {
		case "mat":
			m, err := catalog.Resolve(s.material)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
			e.build.Materials[s.name] = m
		case "let":
			v, err := s.args[0].eval(e.vars)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
			e.vars[s.name] = v
		case "template":
			if _, exists := e.templates[s.name]; exists {
				return fmt.Errorf("line %d: template %q is already defined", s.pos.line, s.name)
			}
			e.templates[s.name] = s
		case "paste":
			tmpl, exists := e.templates[s.name]
			if !exists {
				return fmt.Errorf("line %d: unknown template %q", s.pos.line, s.name)
			}
			if len(s.args) != len(tmpl.params) {
				return fmt.Errorf("line %d: template %q expects %d arguments, got %d", s.pos.line, s.name, len(tmpl.params), len(s.args))
			}
			if e.templateDepth >= MaxNestingDepth {
				return fmt.Errorf("line %d: template paste nesting exceeds %d", s.pos.line, MaxNestingDepth)
			}
			e.templatePastes++
			if e.templatePastes > MaxTemplatePastes {
				return fmt.Errorf("template paste limit exceeded (%d)", MaxTemplatePastes)
			}
			values := make([]int64, len(s.args))
			for i, arg := range s.args {
				value, err := arg.eval(e.vars)
				if err != nil {
					return fmt.Errorf("line %d: %w", s.pos.line, err)
				}
				values[i] = value
			}
			outerVars := e.vars
			e.vars = make(map[string]int64, len(outerVars)+len(values))
			for name, value := range outerVars {
				e.vars[name] = value
			}
			for i, name := range tmpl.params {
				e.vars[name] = values[i]
			}
			e.templateDepth++
			err := e.run(tmpl.body, catalog)
			e.templateDepth--
			e.vars = outerVars
			if err != nil {
				return fmt.Errorf("paste %q at line %d: %w", s.name, s.pos.line, err)
			}
		case "at":
			vals, err := e.values(s.args)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
			old := e.offset
			e.offset = Point{old.X + vals[0], old.Y + vals[1], old.Z + vals[2]}
			err = e.run(s.body, catalog)
			e.offset = old
			if err != nil {
				return err
			}
		case "if":
			condition, err := s.args[0].eval(e.vars)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
			body := s.elseBody
			if condition != 0 {
				body = s.body
			}
			if err := e.run(body, catalog); err != nil {
				return err
			}
		case "for":
			vals, err := e.values(s.args)
			if err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
			start, end := vals[0], vals[1]
			step := 1
			if len(vals) == 3 {
				step = vals[2]
			} else if end < start {
				step = -1
			}
			if step == 0 {
				return fmt.Errorf("line %d: loop step cannot be zero", s.pos.line)
			}
			if start < end && step < 0 || start > end && step > 0 {
				return fmt.Errorf("line %d: loop step moves away from end", s.pos.line)
			}
			old, had := e.vars[s.name]
			for i := start; ; i += step {
				e.build.Loops++
				if e.build.Loops > MaxLoopIterations {
					return fmt.Errorf("loop iteration limit exceeded (%d)", MaxLoopIterations)
				}
				e.vars[s.name] = int64(i)
				if err := e.run(s.body, catalog); err != nil {
					return err
				}
				if i == end {
					break
				}
				if step > 0 && i+step > end || step < 0 && i+step < end {
					break
				}
			}
			if had {
				e.vars[s.name] = old
			} else {
				delete(e.vars, s.name)
			}
		default:
			if err := e.primitive(s); err != nil {
				return fmt.Errorf("line %d: %w", s.pos.line, err)
			}
		}
	}
	return nil
}

func (e *executor) values(args []*expr) ([]int, error) {
	out := make([]int, len(args))
	for i, a := range args {
		v, err := a.eval(e.vars)
		if err != nil {
			return nil, err
		}
		if v < math.MinInt32 || v > math.MaxInt32 {
			return nil, fmt.Errorf("coordinate is outside 32-bit range")
		}
		out[i] = int(v)
	}
	return out, nil
}

func (e *executor) primitive(s statement) error {
	e.build.Primitives++
	if e.build.Primitives > MaxPrimitiveCalls {
		return fmt.Errorf("primitive limit exceeded (%d)", MaxPrimitiveCalls)
	}
	m, ok := e.build.Materials[s.material]
	if !ok {
		return fmt.Errorf("unknown material alias %q", s.material)
	}
	v, err := e.values(s.args)
	if err != nil {
		return err
	}
	switch s.op {
	case "b", "block":
		return e.set(Point{v[0], v[1], v[2]}, m.ID)
	case "box", "hbox":
		t := 0
		if s.op == "hbox" {
			t = 1
			if len(v) > 6 {
				t = v[6]
			}
		}
		return e.box(v[:6], t, m.ID)
	case "sph", "sphere", "hsph":
		t := 0
		if s.op == "hsph" {
			t = 1
			if len(v) > 4 {
				t = v[4]
			}
		}
		return e.sphere(Point{v[0], v[1], v[2]}, v[3], t, m.ID)
	case "ell", "hell":
		t := 0
		if s.op == "hell" {
			t = 1
			if len(v) > 6 {
				t = v[6]
			}
		}
		return e.ellipsoid(Point{v[0], v[1], v[2]}, v[3], v[4], v[5], t, m.ID)
	case "ln", "line":
		r := 0
		if len(v) > 6 {
			r = v[6]
		}
		return e.line(Point{v[0], v[1], v[2]}, Point{v[3], v[4], v[5]}, r, 0, m.ID)
	case "cyl":
		return e.line(Point{v[0], v[1], v[2]}, Point{v[3], v[4], v[5]}, v[6], 0, m.ID)
	case "tube":
		t := 1
		if len(v) > 7 {
			t = v[7]
		}
		return e.line(Point{v[0], v[1], v[2]}, Point{v[3], v[4], v[5]}, v[6], t, m.ID)
	}
	return nil
}

func (e *executor) set(p Point, id string) error {
	p.X += e.offset.X
	p.Y += e.offset.Y
	p.Z += e.offset.Z
	e.build.Writes++
	if e.build.Writes > MaxWriteAttempts {
		return fmt.Errorf("block-write limit exceeded (%d)", MaxWriteAttempts)
	}
	if e.build.Writes&1023 == 0 {
		if err := e.ctx.Err(); err != nil {
			return err
		}
	}
	b := e.build.Bounds
	if p.X < 0 || p.Y < 0 || p.Z < 0 || p.X >= b.X || p.Y >= b.Y || p.Z >= b.Z {
		return fmt.Errorf("block (%d,%d,%d) is outside %dx%dx%d canvas", p.X, p.Y, p.Z, b.X, b.Y, b.Z)
	}
	if id == "minecraft:air" {
		delete(e.build.Blocks, p)
		return nil
	}
	if _, ok := e.build.Blocks[p]; !ok && len(e.build.Blocks) >= MaxOccupiedBlocks {
		return fmt.Errorf("occupied-block limit exceeded (%d)", MaxOccupiedBlocks)
	}
	e.build.Blocks[p] = id
	return nil
}

func ordered(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}
func (e *executor) box(v []int, t int, id string) error {
	x1, x2 := ordered(v[0], v[3])
	y1, y2 := ordered(v[1], v[4])
	z1, z2 := ordered(v[2], v[5])
	if t < 0 {
		return fmt.Errorf("thickness cannot be negative")
	}
	for y := y1; y <= y2; y++ {
		for z := z1; z <= z2; z++ {
			for x := x1; x <= x2; x++ {
				if t > 0 && x-x1 >= t && x2-x >= t && y-y1 >= t && y2-y >= t && z-z1 >= t && z2-z >= t {
					continue
				}
				if err := e.set(Point{x, y, z}, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (e *executor) sphere(c Point, r, t int, id string) error {
	if r < 0 || t < 0 {
		return fmt.Errorf("radius and thickness must be non-negative")
	}
	outer := r * r
	inner := (r - t) * (r - t)
	for y := -r; y <= r; y++ {
		for z := -r; z <= r; z++ {
			for x := -r; x <= r; x++ {
				d := x*x + y*y + z*z
				if d > outer || t > 0 && d < inner {
					continue
				}
				if err := e.set(Point{c.X + x, c.Y + y, c.Z + z}, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (e *executor) ellipsoid(c Point, rx, ry, rz, t int, id string) error {
	if rx <= 0 || ry <= 0 || rz <= 0 || t < 0 {
		return fmt.Errorf("ellipsoid radii must be positive and thickness non-negative")
	}
	for y := -ry; y <= ry; y++ {
		for z := -rz; z <= rz; z++ {
			for x := -rx; x <= rx; x++ {
				d := float64(x*x)/float64(rx*rx) + float64(y*y)/float64(ry*ry) + float64(z*z)/float64(rz*rz)
				if d > 1 {
					continue
				}
				if t > 0 {
					ix, iy, iz := max(rx-t, 1), max(ry-t, 1), max(rz-t, 1)
					inside := float64(x*x)/float64(ix*ix) + float64(y*y)/float64(iy*iy) + float64(z*z)/float64(iz*iz)
					if inside < 1 {
						continue
					}
				}
				if err := e.set(Point{c.X + x, c.Y + y, c.Z + z}, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func (e *executor) line(a, b Point, r, t int, id string) error {
	if r < 0 || t < 0 {
		return fmt.Errorf("radius and thickness must be non-negative")
	}
	dx, dy, dz := b.X-a.X, b.Y-a.Y, b.Z-a.Z
	steps := max(abs(dx), max(abs(dy), abs(dz)))
	if steps == 0 {
		if r == 0 {
			return e.set(a, id)
		}
		return e.sphere(a, r, t, id)
	}
	for i := 0; i <= steps; i++ {
		p := Point{a.X + int(math.Round(float64(dx*i)/float64(steps))), a.Y + int(math.Round(float64(dy*i)/float64(steps))), a.Z + int(math.Round(float64(dz*i)/float64(steps)))}
		if r == 0 {
			if err := e.set(p, id); err != nil {
				return err
			}
		} else if err := e.sphere(p, r, t, id); err != nil {
			return err
		}
	}
	return nil
}
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func boolInt(v bool) int64 {
	if v {
		return 1
	}
	return 0
}
