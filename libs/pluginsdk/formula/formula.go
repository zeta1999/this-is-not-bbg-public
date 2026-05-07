// Package formula provides an Excel-style expression evaluator for
// pluginsdk.Cell grids.
//
// Supported syntax (MVP):
//
//   - Number literals: 1, 1.5, -3.2, 1e-4
//   - Cell references: R<row>C<col>, e.g. R2C3. Refs are
//     case-insensitive on the letters but the numbers are decimal.
//   - Operators: + - * / ^  (standard precedence, ^ right-assoc)
//   - Parens: (…)
//   - Functions: MIN, MAX, ABS, SQRT, LN, EXP, LOG (log10),
//     ROUND(x), ROUND(x, digits).
//
// Cell references resolve to the numeric value of the referenced
// cell. For non-formula cells the value is Cell.Value (float /
// int / numeric string); for formula cells the engine recurses.
// Cycles are detected (Kahn's topological pass) and reported as
// EvalError with Reason="cycle" on every cell in the cycle; those
// cells keep their cached Value.
//
// Non-goals (keep Phase-2.2 tight):
//
//   - No BS / financial functions — plugins compute those in code
//     and expose the result via a "number" cell, then reference it.
//   - No range refs (R1C1:R3C1) — add later if a concrete plugin
//     needs it.
//   - No string functions; formula cells are numeric only.
//   - No side effects; evaluation is pure.
package formula

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/notbbg/notbbg/libs/pluginsdk"
)

// EvalError describes why one cell's formula could not be evaluated.
type EvalError struct {
	Address pluginsdk.CellAddress
	Reason  string // "cycle", "parse", "divzero", "unknown_func", "bad_ref", "arity"
	Detail  string
}

func (e EvalError) Error() string {
	return fmt.Sprintf("R%dC%d: %s: %s", e.Address.Row, e.Address.Col, e.Reason, e.Detail)
}

// Recalc evaluates every formula cell in the grid and writes the
// computed value back into Cell.Value in place. Errors leave the
// affected cell's prior Value untouched and are returned to the
// caller. This is the ergonomic hook plugins use after handling
// an InputEvent — call Recalc, then UpdateCellGrid — so the GUIs
// render fresh formula values.
func Recalc(cells []pluginsdk.Cell) []EvalError {
	values, errs := EvalAll(cells)
	failed := make(map[pluginsdk.CellAddress]struct{}, len(errs))
	for _, e := range errs {
		failed[e.Address] = struct{}{}
	}
	for i := range cells {
		if cells[i].Type != "formula" {
			continue
		}
		if _, bad := failed[cells[i].Address]; bad {
			continue
		}
		if v, ok := values[cells[i].Address]; ok {
			cells[i].Value = v
		}
	}
	return errs
}

// EvalAll evaluates every formula cell in the grid and returns a map
// of address → computed value. Non-formula cells contribute their
// current numeric value but are not recomputed. `errs` collects any
// failures; the caller decides whether to surface them.
func EvalAll(cells []pluginsdk.Cell) (map[pluginsdk.CellAddress]float64, []EvalError) {
	// Index cells by address.
	byAddr := make(map[pluginsdk.CellAddress]*pluginsdk.Cell, len(cells))
	for i := range cells {
		byAddr[cells[i].Address] = &cells[i]
	}

	// Parse every formula once up-front. Bad parses are terminal for
	// that cell — we still evaluate the rest.
	type parsed struct {
		cell *pluginsdk.Cell
		root expr
		deps []pluginsdk.CellAddress
	}
	parseds := make(map[pluginsdk.CellAddress]*parsed, len(cells))
	var errs []EvalError
	for i := range cells {
		c := &cells[i]
		if c.Type != "formula" {
			continue
		}
		tree, err := parse(c.Expression)
		if err != nil {
			errs = append(errs, EvalError{Address: c.Address, Reason: "parse", Detail: err.Error()})
			continue
		}
		parseds[c.Address] = &parsed{cell: c, root: tree, deps: collectRefs(tree)}
	}

	// Topological sort via Kahn's: vertices = formula cells, edges
	// dep→dependent. Any cell in a cycle is reported + skipped.
	inDeg := make(map[pluginsdk.CellAddress]int, len(parseds))
	out := make(map[pluginsdk.CellAddress][]pluginsdk.CellAddress, len(parseds))
	for addr := range parseds {
		inDeg[addr] = 0
	}
	for addr, p := range parseds {
		for _, d := range p.deps {
			if _, ok := parseds[d]; !ok {
				continue // dep is a non-formula cell; no edge in the graph
			}
			out[d] = append(out[d], addr)
			inDeg[addr]++
		}
	}
	queue := make([]pluginsdk.CellAddress, 0, len(parseds))
	for addr, d := range inDeg {
		if d == 0 {
			queue = append(queue, addr)
		}
	}
	order := make([]pluginsdk.CellAddress, 0, len(parseds))
	for len(queue) > 0 {
		a := queue[0]
		queue = queue[1:]
		order = append(order, a)
		for _, next := range out[a] {
			inDeg[next]--
			if inDeg[next] == 0 {
				queue = append(queue, next)
			}
		}
	}
	if len(order) != len(parseds) {
		for addr, d := range inDeg {
			if d > 0 {
				errs = append(errs, EvalError{Address: addr, Reason: "cycle", Detail: "cell is part of a dependency cycle"})
			}
		}
	}

	values := make(map[pluginsdk.CellAddress]float64, len(cells))
	// Seed non-formula cell values.
	for _, c := range cells {
		if c.Type == "formula" {
			continue
		}
		if v, ok := asFloat(c.Value); ok {
			values[c.Address] = v
		}
	}
	// Evaluate in topological order.
	for _, addr := range order {
		p := parseds[addr]
		v, err := eval(p.root, values)
		if err != nil {
			if ee, ok := err.(EvalError); ok {
				ee.Address = addr
				errs = append(errs, ee)
			} else {
				errs = append(errs, EvalError{Address: addr, Reason: "eval", Detail: err.Error()})
			}
			continue
		}
		values[addr] = v
	}
	return values, errs
}

// --- expression AST ---

type expr interface{ isExpr() }

type numLit struct{ v float64 }
type cellRef struct{ row, col uint32 }
type binOp struct {
	op       byte // '+', '-', '*', '/', '^'
	lhs, rhs expr
}
type unaryOp struct {
	op  byte // '-'
	arg expr
}
type funcCall struct {
	name string
	args []expr
}

func (numLit) isExpr()   {}
func (cellRef) isExpr()  {}
func (binOp) isExpr()    {}
func (unaryOp) isExpr()  {}
func (funcCall) isExpr() {}

// --- tokenizer ---

type tokKind int

const (
	tkNum tokKind = iota
	tkIdent
	tkLParen
	tkRParen
	tkComma
	tkPlus
	tkMinus
	tkStar
	tkSlash
	tkCaret
	tkEOF
)

type token struct {
	kind  tokKind
	text  string
	num   float64
}

type tokenizer struct {
	s   string
	pos int
}

func (tz *tokenizer) next() (token, error) {
	for tz.pos < len(tz.s) && (tz.s[tz.pos] == ' ' || tz.s[tz.pos] == '\t') {
		tz.pos++
	}
	if tz.pos >= len(tz.s) {
		return token{kind: tkEOF}, nil
	}
	c := tz.s[tz.pos]
	switch c {
	case '(':
		tz.pos++
		return token{kind: tkLParen}, nil
	case ')':
		tz.pos++
		return token{kind: tkRParen}, nil
	case ',':
		tz.pos++
		return token{kind: tkComma}, nil
	case '+':
		tz.pos++
		return token{kind: tkPlus}, nil
	case '-':
		tz.pos++
		return token{kind: tkMinus}, nil
	case '*':
		tz.pos++
		return token{kind: tkStar}, nil
	case '/':
		tz.pos++
		return token{kind: tkSlash}, nil
	case '^':
		tz.pos++
		return token{kind: tkCaret}, nil
	}
	if isDigit(c) || c == '.' {
		return tz.number()
	}
	if isAlpha(c) {
		return tz.ident()
	}
	return token{}, fmt.Errorf("unexpected %q at %d", c, tz.pos)
}

func (tz *tokenizer) number() (token, error) {
	start := tz.pos
	for tz.pos < len(tz.s) && (isDigit(tz.s[tz.pos]) || tz.s[tz.pos] == '.') {
		tz.pos++
	}
	// exponent
	if tz.pos < len(tz.s) && (tz.s[tz.pos] == 'e' || tz.s[tz.pos] == 'E') {
		tz.pos++
		if tz.pos < len(tz.s) && (tz.s[tz.pos] == '+' || tz.s[tz.pos] == '-') {
			tz.pos++
		}
		for tz.pos < len(tz.s) && isDigit(tz.s[tz.pos]) {
			tz.pos++
		}
	}
	text := tz.s[start:tz.pos]
	v, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return token{}, fmt.Errorf("bad number %q", text)
	}
	return token{kind: tkNum, text: text, num: v}, nil
}

func (tz *tokenizer) ident() (token, error) {
	start := tz.pos
	for tz.pos < len(tz.s) && (isAlpha(tz.s[tz.pos]) || isDigit(tz.s[tz.pos])) {
		tz.pos++
	}
	return token{kind: tkIdent, text: tz.s[start:tz.pos]}, nil
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_' }

// --- parser ---
// Grammar:
//   expr   = term  (('+'|'-') term)*
//   term   = power (('*'|'/') power)*
//   power  = unary ('^' power)?     -- right-assoc
//   unary  = ('+'|'-')? atom
//   atom   = num | ref | ident '(' args? ')' | '(' expr ')'
//   args   = expr (',' expr)*
//   ref    = 'R' num 'C' num

type parser struct {
	tz  *tokenizer
	cur token
}

func parse(src string) (expr, error) {
	src = strings.TrimSpace(src)
	src = strings.TrimPrefix(src, "=")
	tz := &tokenizer{s: src}
	p := &parser{tz: tz}
	if err := p.advance(); err != nil {
		return nil, err
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.cur.kind != tkEOF {
		return nil, fmt.Errorf("trailing tokens after expression")
	}
	return e, nil
}

func (p *parser) advance() error {
	t, err := p.tz.next()
	if err != nil {
		return err
	}
	p.cur = t
	return nil
}

func (p *parser) parseExpr() (expr, error) {
	lhs, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.cur.kind == tkPlus || p.cur.kind == tkMinus {
		op := byte('+')
		if p.cur.kind == tkMinus {
			op = '-'
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		rhs, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		lhs = binOp{op: op, lhs: lhs, rhs: rhs}
	}
	return lhs, nil
}

func (p *parser) parseTerm() (expr, error) {
	lhs, err := p.parsePower()
	if err != nil {
		return nil, err
	}
	for p.cur.kind == tkStar || p.cur.kind == tkSlash {
		op := byte('*')
		if p.cur.kind == tkSlash {
			op = '/'
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		rhs, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		lhs = binOp{op: op, lhs: lhs, rhs: rhs}
	}
	return lhs, nil
}

func (p *parser) parsePower() (expr, error) {
	lhs, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	if p.cur.kind == tkCaret {
		if err := p.advance(); err != nil {
			return nil, err
		}
		// right-assoc: parsePower recursively
		rhs, err := p.parsePower()
		if err != nil {
			return nil, err
		}
		return binOp{op: '^', lhs: lhs, rhs: rhs}, nil
	}
	return lhs, nil
}

func (p *parser) parseUnary() (expr, error) {
	if p.cur.kind == tkPlus {
		if err := p.advance(); err != nil {
			return nil, err
		}
		return p.parseUnary()
	}
	if p.cur.kind == tkMinus {
		if err := p.advance(); err != nil {
			return nil, err
		}
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return unaryOp{op: '-', arg: inner}, nil
	}
	return p.parseAtom()
}

func (p *parser) parseAtom() (expr, error) {
	switch p.cur.kind {
	case tkNum:
		v := p.cur.num
		if err := p.advance(); err != nil {
			return nil, err
		}
		return numLit{v: v}, nil
	case tkLParen:
		if err := p.advance(); err != nil {
			return nil, err
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if p.cur.kind != tkRParen {
			return nil, fmt.Errorf("expected ')'")
		}
		if err := p.advance(); err != nil {
			return nil, err
		}
		return e, nil
	case tkIdent:
		name := strings.ToUpper(p.cur.text)
		if err := p.advance(); err != nil {
			return nil, err
		}
		// Cell ref R<row>C<col>?
		if ref, ok := tryParseCellRef(name, p); ok {
			return ref, nil
		}
		// Function call?
		if p.cur.kind == tkLParen {
			if err := p.advance(); err != nil {
				return nil, err
			}
			var args []expr
			if p.cur.kind != tkRParen {
				for {
					a, err := p.parseExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if p.cur.kind != tkComma {
						break
					}
					if err := p.advance(); err != nil {
						return nil, err
					}
				}
			}
			if p.cur.kind != tkRParen {
				return nil, fmt.Errorf("expected ')' after arguments of %s", name)
			}
			if err := p.advance(); err != nil {
				return nil, err
			}
			return funcCall{name: name, args: args}, nil
		}
		return nil, fmt.Errorf("unknown identifier %q", name)
	}
	return nil, fmt.Errorf("unexpected token %q", p.cur.text)
}

// tryParseCellRef recognises the `R<row>C<col>` form where the `R`
// and `C` are already consumed as a single identifier (e.g. "R2C3").
// It accepts "R" followed by digits followed by "C" followed by
// digits; anything else returns ok=false and the caller continues
// with the identifier path.
func tryParseCellRef(name string, _ *parser) (cellRef, bool) {
	if len(name) < 4 || (name[0] != 'R' && name[0] != 'r') {
		return cellRef{}, false
	}
	// Walk chars: R<digits>C<digits>
	i := 1
	rStart := i
	for i < len(name) && isDigit(name[i]) {
		i++
	}
	if i == rStart || i >= len(name) || (name[i] != 'C' && name[i] != 'c') {
		return cellRef{}, false
	}
	cStart := i + 1
	j := cStart
	for j < len(name) && isDigit(name[j]) {
		j++
	}
	if j == cStart || j != len(name) {
		return cellRef{}, false
	}
	r, _ := strconv.ParseUint(name[rStart:i], 10, 32)
	c, _ := strconv.ParseUint(name[cStart:j], 10, 32)
	return cellRef{row: uint32(r), col: uint32(c)}, true
}

// --- evaluator ---

func collectRefs(e expr) []pluginsdk.CellAddress {
	var out []pluginsdk.CellAddress
	var walk func(expr)
	walk = func(e expr) {
		switch v := e.(type) {
		case cellRef:
			out = append(out, pluginsdk.CellAddress{Row: v.row, Col: v.col})
		case binOp:
			walk(v.lhs)
			walk(v.rhs)
		case unaryOp:
			walk(v.arg)
		case funcCall:
			for _, a := range v.args {
				walk(a)
			}
		}
	}
	walk(e)
	return out
}

func eval(e expr, values map[pluginsdk.CellAddress]float64) (float64, error) {
	switch v := e.(type) {
	case numLit:
		return v.v, nil
	case cellRef:
		val, ok := values[pluginsdk.CellAddress{Row: v.row, Col: v.col}]
		if !ok {
			return 0, EvalError{Reason: "bad_ref", Detail: fmt.Sprintf("R%dC%d has no value", v.row, v.col)}
		}
		return val, nil
	case binOp:
		l, err := eval(v.lhs, values)
		if err != nil {
			return 0, err
		}
		r, err := eval(v.rhs, values)
		if err != nil {
			return 0, err
		}
		switch v.op {
		case '+':
			return l + r, nil
		case '-':
			return l - r, nil
		case '*':
			return l * r, nil
		case '/':
			if r == 0 {
				return 0, EvalError{Reason: "divzero", Detail: "division by zero"}
			}
			return l / r, nil
		case '^':
			return math.Pow(l, r), nil
		}
		return 0, fmt.Errorf("bad operator")
	case unaryOp:
		x, err := eval(v.arg, values)
		if err != nil {
			return 0, err
		}
		if v.op == '-' {
			return -x, nil
		}
		return x, nil
	case funcCall:
		return evalFunc(v, values)
	}
	return 0, fmt.Errorf("unexpected expr")
}

func evalFunc(f funcCall, values map[pluginsdk.CellAddress]float64) (float64, error) {
	args := make([]float64, len(f.args))
	for i, a := range f.args {
		v, err := eval(a, values)
		if err != nil {
			return 0, err
		}
		args[i] = v
	}
	check := func(min, max int) error {
		if len(args) < min || (max > 0 && len(args) > max) {
			return EvalError{Reason: "arity", Detail: fmt.Sprintf("%s takes %d..%d args, got %d", f.name, min, max, len(args))}
		}
		return nil
	}
	switch f.name {
	case "ABS":
		if err := check(1, 1); err != nil {
			return 0, err
		}
		return math.Abs(args[0]), nil
	case "SQRT":
		if err := check(1, 1); err != nil {
			return 0, err
		}
		return math.Sqrt(args[0]), nil
	case "LN":
		if err := check(1, 1); err != nil {
			return 0, err
		}
		return math.Log(args[0]), nil
	case "LOG":
		if err := check(1, 1); err != nil {
			return 0, err
		}
		return math.Log10(args[0]), nil
	case "EXP":
		if err := check(1, 1); err != nil {
			return 0, err
		}
		return math.Exp(args[0]), nil
	case "MIN":
		if err := check(1, 0); err != nil {
			return 0, err
		}
		m := args[0]
		for _, v := range args[1:] {
			if v < m {
				m = v
			}
		}
		return m, nil
	case "MAX":
		if err := check(1, 0); err != nil {
			return 0, err
		}
		m := args[0]
		for _, v := range args[1:] {
			if v > m {
				m = v
			}
		}
		return m, nil
	case "ROUND":
		if err := check(1, 2); err != nil {
			return 0, err
		}
		digits := 0
		if len(args) == 2 {
			digits = int(args[1])
		}
		scale := math.Pow(10, float64(digits))
		return math.Round(args[0]*scale) / scale, nil
	case "SUM":
		if err := check(1, 0); err != nil {
			return 0, err
		}
		sum := 0.0
		for _, v := range args {
			sum += v
		}
		return sum, nil
	case "AVG", "AVERAGE":
		if err := check(1, 0); err != nil {
			return 0, err
		}
		sum := 0.0
		for _, v := range args {
			sum += v
		}
		return sum / float64(len(args)), nil
	}
	return 0, EvalError{Reason: "unknown_func", Detail: f.name}
}

// asFloat converts a Cell.Value (any) to float64. Returns ok=false
// when the value is nil, a non-numeric string, or an unsupported
// type. Matches pluginsdk.CellValue semantics so plugins that
// populate Value via that helper round-trip identically here.
func asFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	case bool:
		if x {
			return 1, true
		}
		return 0, true
	case nil:
		return 0, false
	}
	return 0, false
}
