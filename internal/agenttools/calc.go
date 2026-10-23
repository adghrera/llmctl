package agenttools

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// --- calculator -------------------------------------------------------------
// Recursive-descent over + - * / % ^ and parentheses. No eval, no refs.

type CalcTool struct{}

func (t *CalcTool) Name() string { return "calculator" }

func (t *CalcTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	expr, err := parseArg(argsJSON, "expression", "expr", "input", "query")
	if err != nil {
		return "", nil, err
	}
	expr = strings.NewReplacer("×", "*", "÷", "/", "−", "-", ",", "", " ", "", "^", "^").Replace(expr)
	if strings.TrimSpace(expr) == "" {
		return "", nil, fmt.Errorf("calculator: expression required")
	}
	p := &calcParser{src: expr}
	v, err := p.parseExpr()
	if err != nil {
		return "", nil, fmt.Errorf("calculator: %w (in %q)", err, expr)
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return "", nil, fmt.Errorf("calculator: unexpected %q (in %q)", p.src[p.pos:], expr)
	}
	res := strconv.FormatFloat(v, 'f', -1, 64)
	return fmt.Sprintf("%s = %s", expr, res), []map[string]any{{"expression": expr, "result": res}}, nil
}

type calcParser struct {
	src string
	pos int
}

func (p *calcParser) skipSpace() {
	for p.pos < len(p.src) && unicode.IsSpace(rune(p.src[p.pos])) {
		p.pos++
	}
}

func (p *calcParser) parseExpr() (float64, error) {
	v, err := p.parseTerm()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return v, nil
		}
		switch p.src[p.pos] {
		case '+':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v += r
		case '-':
			p.pos++
			r, err := p.parseTerm()
			if err != nil {
				return 0, err
			}
			v -= r
		default:
			return v, nil
		}
	}
}

func (p *calcParser) parseTerm() (float64, error) {
	v, err := p.parseUnary()
	if err != nil {
		return 0, err
	}
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return v, nil
		}
		switch p.src[p.pos] {
		case '*':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			v *= r
		case '/':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("division by zero")
			}
			v /= r
		case '%':
			p.pos++
			r, err := p.parseUnary()
			if err != nil {
				return 0, err
			}
			if r == 0 {
				return 0, fmt.Errorf("modulo by zero")
			}
			v = float64(int64(v) % int64(r))
		default:
			return v, nil
		}
	}
}

func (p *calcParser) parseUnary() (float64, error) {
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '-' {
		p.pos++
		v, err := p.parseUnary()
		return -v, err
	}
	if p.pos < len(p.src) && p.src[p.pos] == '+' {
		p.pos++
	}
	return p.parsePower()
}

func (p *calcParser) parsePower() (float64, error) {
	base, err := p.parseAtom()
	if err != nil {
		return 0, err
	}
	p.skipSpace()
	if p.pos < len(p.src) && p.src[p.pos] == '^' {
		p.pos++
		exp, err := p.parseUnary() // right-assoc
		if err != nil {
			return 0, err
		}
		v := 1.0
		// integer exponent fast path; float via exp/log would need math pkg
		if exp == float64(int64(exp)) {
			n := int64(exp)
			neg := n < 0
			if neg {
				n = -n
			}
			for i := int64(0); i < n; i++ {
				v *= base
			}
			if neg {
				if v == 0 {
					return 0, fmt.Errorf("division by zero")
				}
				v = 1 / v
			}
			return v, nil
		}
		return 0, fmt.Errorf("only integer exponents supported")
	}
	return base, nil
}

func (p *calcParser) parseAtom() (float64, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0, fmt.Errorf("unexpected end of expression")
	}
	if p.src[p.pos] == '(' {
		p.pos++
		v, err := p.parseExpr()
		if err != nil {
			return 0, err
		}
		p.skipSpace()
		if p.pos >= len(p.src) || p.src[p.pos] != ')' {
			return 0, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return v, nil
	}
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9' || p.src[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, fmt.Errorf("expected a number at %q", p.src[min(p.pos+6, len(p.src)):])
	}
	return strconv.ParseFloat(p.src[start:p.pos], 64)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// --- current_time -------------------------------------------------------------

type TimeTool struct{}

func (t *TimeTool) Name() string { return "current_time" }

func (t *TimeTool) Execute(ctx context.Context, argsJSON string) (string, any, error) {
	now := time.Now()
	out := fmt.Sprintf("Current time: %s (local, %s) | %s (UTC)",
		now.Format("Monday, 2006-01-02 15:04:05 MST"), now.Format("MST"),
		now.UTC().Format("2006-01-02 15:04:05 UTC"))
	return out, []map[string]any{{"utc": now.UTC().Format(time.RFC3339), "local": now.Format(time.RFC3339)}}, nil
}
