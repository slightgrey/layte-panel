// Package nginx parses nginx config files into a tree of directives. It is
// only used to READ existing configs (to discover sites); Layte Panel never
// rewrites files it didn't create.
package nginx

import (
	"fmt"
	"strings"
)

// Directive is one statement: `name arg1 arg2;` or `name args { block }`.
type Directive struct {
	Name  string
	Args  []string
	Block []*Directive // nil for simple directives
	Line  int
}

// Arg returns argument i or "".
func (d *Directive) Arg(i int) string {
	if i < len(d.Args) {
		return d.Args[i]
	}
	return ""
}

// Find returns the direct children named name.
func Find(ds []*Directive, name string) []*Directive {
	var out []*Directive
	for _, d := range ds {
		if d.Name == name {
			out = append(out, d)
		}
	}
	return out
}

// First returns the first direct child named name, or nil.
func First(ds []*Directive, name string) *Directive {
	for _, d := range ds {
		if d.Name == name {
			return d
		}
	}
	return nil
}

// Walk calls fn for every directive, depth first.
func Walk(ds []*Directive, fn func(*Directive)) {
	for _, d := range ds {
		fn(d)
		Walk(d.Block, fn)
	}
}

type token struct {
	text   string
	quoted bool
	line   int
}

// Parse parses a config file. It handles comments, single/double quoted
// strings with escapes, and nested blocks.
func Parse(src string) ([]*Directive, error) {
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{toks: toks}
	ds, err := p.block(false)
	if err != nil {
		return nil, err
	}
	return ds, nil
}

type parser struct {
	toks []token
	pos  int
}

func (p *parser) block(nested bool) ([]*Directive, error) {
	var out []*Directive
	for p.pos < len(p.toks) {
		t := p.toks[p.pos]
		if !t.quoted && t.text == "}" {
			if !nested {
				return nil, fmt.Errorf("line %d: unexpected }", t.line)
			}
			p.pos++
			return out, nil
		}
		if !t.quoted && (t.text == ";" || t.text == "{") {
			return nil, fmt.Errorf("line %d: unexpected %q", t.line, t.text)
		}
		d := &Directive{Name: t.text, Line: t.line}
		p.pos++
		for {
			if p.pos >= len(p.toks) {
				return nil, fmt.Errorf("line %d: %q is missing ; or {", d.Line, d.Name)
			}
			t := p.toks[p.pos]
			p.pos++
			if !t.quoted && t.text == ";" {
				break
			}
			if !t.quoted && t.text == "{" {
				child, err := p.block(true)
				if err != nil {
					return nil, err
				}
				d.Block = child
				if d.Block == nil {
					d.Block = []*Directive{}
				}
				break
			}
			if !t.quoted && t.text == "}" {
				return nil, fmt.Errorf("line %d: unexpected } in %q", t.line, d.Name)
			}
			d.Args = append(d.Args, t.text)
		}
		out = append(out, d)
	}
	if nested {
		return nil, fmt.Errorf("unexpected end of file: missing }")
	}
	return out, nil
}

func tokenize(src string) ([]token, error) {
	var toks []token
	line := 1
	i := 0
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n':
			line++
			i++
		case c == ' ' || c == '\t' || c == '\r':
			i++
		case c == '#':
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case c == ';' || c == '{' || c == '}':
			toks = append(toks, token{text: string(c), line: line})
			i++
		case c == '"' || c == '\'':
			quote := c
			start := line
			var b strings.Builder
			i++
			closed := false
			for i < len(src) {
				ch := src[i]
				if ch == '\\' && i+1 < len(src) {
					b.WriteByte(src[i+1])
					i += 2
					continue
				}
				if ch == quote {
					closed = true
					i++
					break
				}
				if ch == '\n' {
					line++
				}
				b.WriteByte(ch)
				i++
			}
			if !closed {
				return nil, fmt.Errorf("line %d: unterminated string", start)
			}
			toks = append(toks, token{text: b.String(), quoted: true, line: start})
		default:
			start := i
			for i < len(src) {
				ch := src[i]
				if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' || ch == ';' || ch == '{' || ch == '}' {
					break
				}
				// A "#" inside a word (e.g. a regex) isn't a comment.
				if ch == '\\' && i+1 < len(src) {
					i += 2
					continue
				}
				i++
			}
			toks = append(toks, token{text: src[start:i], line: line})
		}
	}
	return toks, nil
}
