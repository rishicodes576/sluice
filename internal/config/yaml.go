package config

import (
	"strconv"
	"strings"
)

// This file implements a deliberately small YAML parser supporting the subset
// Sluice's config uses: indentation-based mappings and sequences, block and
// inline (`[a, b]`) sequences, scalars (string/int/float/bool/null) and inline
// comments. It parses into generic Go values (map[string]any, []any, scalars)
// which are then marshalled to JSON and decoded into the typed Config. This
// keeps the gateway core free of third-party modules.

type yamlLine struct {
	indent int
	text   string
}

type yamlParser struct {
	lines []yamlLine
	pos   int
}

// parseYAML parses a YAML document into a generic value.
func parseYAML(src string) (any, error) {
	p := &yamlParser{}
	for _, raw := range strings.Split(src, "\n") {
		content := stripComment(raw)
		if strings.TrimSpace(content) == "" {
			continue
		}
		if strings.TrimSpace(content) == "---" {
			continue
		}
		indent := len(content) - len(strings.TrimLeft(content, " "))
		p.lines = append(p.lines, yamlLine{indent: indent, text: strings.TrimRight(content[indent:], " ")})
	}
	if len(p.lines) == 0 {
		return map[string]any{}, nil
	}
	return p.parseNode(p.lines[0].indent), nil
}

// stripComment removes an inline/full-line comment that is not inside quotes.
func stripComment(s string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble && (i == 0 || s[i-1] == ' ' || s[i-1] == '\t') {
				return s[:i]
			}
		}
	}
	return s
}

func (p *yamlParser) parseNode(indent int) any {
	if p.pos >= len(p.lines) {
		return nil
	}
	ln := p.lines[p.pos]
	if ln.text == "-" || strings.HasPrefix(ln.text, "- ") {
		return p.parseSeq(ln.indent)
	}
	return p.parseMap(indent)
}

func (p *yamlParser) parseMap(indent int) any {
	m := map[string]any{}
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent {
			break
		}
		key, rest, ok := splitKey(ln.text)
		if !ok {
			break
		}
		p.pos++
		if rest == "" {
			// Nested block if the following line is more deeply indented.
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				m[key] = p.parseNode(p.lines[p.pos].indent)
			} else {
				m[key] = nil
			}
		} else {
			m[key] = scalar(rest)
		}
	}
	return m
}

func (p *yamlParser) parseSeq(indent int) any {
	var seq []any
	for p.pos < len(p.lines) {
		ln := p.lines[p.pos]
		if ln.indent != indent || (ln.text != "-" && !strings.HasPrefix(ln.text, "- ")) {
			break
		}
		content := ""
		if ln.text != "-" {
			content = strings.TrimSpace(ln.text[2:])
		}
		contentIndent := indent + 2
		switch {
		case content == "":
			p.pos++
			if p.pos < len(p.lines) && p.lines[p.pos].indent > indent {
				seq = append(seq, p.parseNode(p.lines[p.pos].indent))
			} else {
				seq = append(seq, nil)
			}
		case isMapEntry(content):
			// Treat "- key: value" as the first entry of a map item; rewrite the
			// line to the content's virtual indent and parse it as a mapping.
			p.lines[p.pos] = yamlLine{indent: contentIndent, text: content}
			seq = append(seq, p.parseMap(contentIndent))
		default:
			p.pos++
			seq = append(seq, scalar(content))
		}
	}
	return seq
}

// splitKey splits "key: value" at the first colon followed by space or EOL.
func splitKey(text string) (key, rest string, ok bool) {
	for i := 0; i < len(text); i++ {
		if text[i] == ':' && (i == len(text)-1 || text[i+1] == ' ') {
			key = unquote(strings.TrimSpace(text[:i]))
			if i+1 < len(text) {
				rest = strings.TrimSpace(text[i+1:])
			}
			return key, rest, true
		}
	}
	return "", "", false
}

func isMapEntry(content string) bool {
	_, _, ok := splitKey(content)
	return ok
}

// scalar converts a YAML scalar token into a typed Go value.
func scalar(s string) any {
	s = strings.TrimSpace(s)
	if s == "" || s == "null" || s == "~" {
		return nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		return flowSeq(s[1 : len(s)-1])
	}
	if strings.HasPrefix(s, "\"") || strings.HasPrefix(s, "'") {
		return unquote(s)
	}
	switch s {
	case "true", "yes", "on":
		return true
	case "false", "no", "off":
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// flowSeq parses a comma-separated inline sequence body.
func flowSeq(body string) []any {
	body = strings.TrimSpace(body)
	if body == "" {
		return []any{}
	}
	var out []any
	for _, part := range splitTopComma(body) {
		out = append(out, scalar(strings.TrimSpace(part)))
	}
	return out
}

// splitTopComma splits on commas not inside quotes or nested brackets.
func splitTopComma(s string) []string {
	var parts []string
	depth := 0
	inSingle, inDouble := false, false
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '[', '{':
			if !inSingle && !inDouble {
				depth++
			}
		case ']', '}':
			if !inSingle && !inDouble {
				depth--
			}
		case ',':
			if depth == 0 && !inSingle && !inDouble {
				parts = append(parts, s[start:i])
				start = i + 1
			}
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
