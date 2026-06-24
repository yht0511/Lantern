package config

import (
	"fmt"
	"strconv"
	"strings"
)

type yamlToken struct {
	Line    int
	Indent  int
	List    bool
	Content string
}

func parseYAML(data []byte) (map[string]any, error) {
	tokens, err := tokenizeYAML(string(data))
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return map[string]any{}, nil
	}
	idx := 0
	root, err := parseBlock(tokens, &idx, tokens[0].Indent)
	if err != nil {
		return nil, err
	}
	m, ok := root.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("root must be a mapping")
	}
	return m, nil
}

func tokenizeYAML(s string) ([]yamlToken, error) {
	var out []yamlToken
	lines := strings.Split(s, "\n")
	for i, raw := range lines {
		line := stripYAMLComment(raw)
		if strings.TrimSpace(line) == "" {
			continue
		}
		if strings.Contains(line, "\t") {
			return nil, fmt.Errorf("line %d: tabs are not supported in indentation", i+1)
		}
		indent := len(line) - len(strings.TrimLeft(line, " "))
		content := strings.TrimSpace(line)
		tok := yamlToken{Line: i + 1, Indent: indent, Content: content}
		if strings.HasPrefix(content, "- ") {
			tok.List = true
			tok.Content = strings.TrimSpace(strings.TrimPrefix(content, "- "))
		} else if content == "-" {
			tok.List = true
			tok.Content = ""
		}
		out = append(out, tok)
	}
	return out, nil
}

func parseBlock(tokens []yamlToken, idx *int, indent int) (any, error) {
	if *idx >= len(tokens) {
		return map[string]any{}, nil
	}
	if tokens[*idx].Indent != indent {
		return nil, fmt.Errorf("line %d: expected indent %d, got %d", tokens[*idx].Line, indent, tokens[*idx].Indent)
	}
	if tokens[*idx].List {
		return parseList(tokens, idx, indent)
	}
	return parseMap(tokens, idx, indent)
}

func parseMap(tokens []yamlToken, idx *int, indent int) (map[string]any, error) {
	m := map[string]any{}
	for *idx < len(tokens) {
		tok := tokens[*idx]
		if tok.Indent < indent {
			break
		}
		if tok.Indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indent %d", tok.Line, tok.Indent)
		}
		if tok.List {
			break
		}
		key, raw, hasValue, err := splitKeyValue(tok)
		if err != nil {
			return nil, err
		}
		*idx++
		if hasValue {
			m[key] = parseScalar(raw)
			continue
		}
		if *idx < len(tokens) && tokens[*idx].Indent > indent {
			child, err := parseBlock(tokens, idx, tokens[*idx].Indent)
			if err != nil {
				return nil, err
			}
			m[key] = child
		} else {
			m[key] = map[string]any{}
		}
	}
	return m, nil
}

func parseList(tokens []yamlToken, idx *int, indent int) ([]any, error) {
	var out []any
	for *idx < len(tokens) {
		tok := tokens[*idx]
		if tok.Indent < indent {
			break
		}
		if tok.Indent > indent {
			return nil, fmt.Errorf("line %d: unexpected indent %d", tok.Line, tok.Indent)
		}
		if !tok.List {
			break
		}
		*idx++
		if tok.Content == "" {
			if *idx < len(tokens) && tokens[*idx].Indent > indent {
				child, err := parseBlock(tokens, idx, tokens[*idx].Indent)
				if err != nil {
					return nil, err
				}
				out = append(out, child)
			} else {
				out = append(out, "")
			}
			continue
		}
		if looksLikeKeyValue(tok.Content) {
			key, raw, hasValue, err := splitInlineKeyValue(tok)
			if err != nil {
				return nil, err
			}
			item := map[string]any{}
			if hasValue {
				item[key] = parseScalar(raw)
			} else if *idx < len(tokens) && tokens[*idx].Indent > indent {
				child, err := parseBlock(tokens, idx, tokens[*idx].Indent)
				if err != nil {
					return nil, err
				}
				item[key] = child
			} else {
				item[key] = map[string]any{}
			}
			if *idx < len(tokens) && tokens[*idx].Indent > indent {
				child, err := parseBlock(tokens, idx, tokens[*idx].Indent)
				if err != nil {
					return nil, err
				}
				if childMap, ok := child.(map[string]any); ok {
					for k, v := range childMap {
						item[k] = v
					}
				} else {
					return nil, fmt.Errorf("line %d: list item continuation must be a mapping", tok.Line)
				}
			}
			out = append(out, item)
			continue
		}
		out = append(out, parseScalar(tok.Content))
	}
	return out, nil
}

func stripYAMLComment(s string) string {
	inSingle := false
	inDouble := false
	escaped := false
	for i, r := range s {
		switch r {
		case '\\':
			if inDouble {
				escaped = !escaped
			}
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
			escaped = false
		case '"':
			if !inSingle && !escaped {
				inDouble = !inDouble
			}
			escaped = false
		case '#':
			if !inSingle && !inDouble {
				return s[:i]
			}
			escaped = false
		default:
			escaped = false
		}
	}
	return s
}

func splitKeyValue(tok yamlToken) (string, string, bool, error) {
	key, raw, ok := splitColon(tok.Content)
	if !ok {
		return "", "", false, fmt.Errorf("line %d: expected key: value", tok.Line)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("line %d: empty key", tok.Line)
	}
	raw = strings.TrimSpace(raw)
	return key, raw, raw != "", nil
}

func splitInlineKeyValue(tok yamlToken) (string, string, bool, error) {
	key, raw, ok := splitColon(tok.Content)
	if !ok {
		return "", "", false, fmt.Errorf("line %d: expected key: value", tok.Line)
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false, fmt.Errorf("line %d: empty key", tok.Line)
	}
	raw = strings.TrimSpace(raw)
	return key, raw, raw != "", nil
}

func splitColon(s string) (string, string, bool) {
	inSingle := false
	inDouble := false
	for i, r := range s {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case ':':
			if !inSingle && !inDouble {
				return s[:i], s[i+1:], true
			}
		}
	}
	return "", "", false
}

func looksLikeKeyValue(s string) bool {
	key, _, ok := splitColon(s)
	if !ok {
		return false
	}
	key = strings.TrimSpace(key)
	return key != "" && !strings.ContainsAny(key, " \t")
}

func parseScalar(raw string) any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw == "[]" {
		return []any{}
	}
	if raw == "{}" {
		return map[string]any{}
	}
	if raw == "true" || raw == "True" || raw == "yes" || raw == "on" {
		return true
	}
	if raw == "false" || raw == "False" || raw == "no" || raw == "off" {
		return false
	}
	if strings.EqualFold(raw, "null") {
		return nil
	}
	if strings.HasPrefix(raw, `"`) && strings.HasSuffix(raw, `"`) && len(raw) >= 2 {
		if v, err := strconv.Unquote(raw); err == nil {
			return v
		}
	}
	if strings.HasPrefix(raw, `'`) && strings.HasSuffix(raw, `'`) && len(raw) >= 2 {
		return strings.ReplaceAll(raw[1:len(raw)-1], "''", "'")
	}
	if i, err := strconv.Atoi(raw); err == nil {
		return i
	}
	return raw
}
