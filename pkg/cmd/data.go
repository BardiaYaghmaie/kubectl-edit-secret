package cmd

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if !unicode.IsPrint(r) && !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

func valueNode(v []byte, literal bool) *yaml.Node {
	if !isText(v) {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!binary", Value: base64.StdEncoding.EncodeToString(v)}
	}
	n := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: string(v)}
	if literal && strings.Contains(n.Value, "\n") {
		n.Style = yaml.LiteralStyle
	}
	return n
}

// prettyJSON returns an indented form of compact JSON objects/arrays. Only compact
// input is accepted, so compacting the edited value restores the original bytes.
func prettyJSON(v []byte) ([]byte, bool) {
	if len(v) == 0 || (v[0] != '{' && v[0] != '[') || !json.Valid(v) {
		return nil, false
	}
	var c bytes.Buffer
	if json.Compact(&c, v) != nil || !bytes.Equal(c.Bytes(), v) {
		return nil, false
	}
	var p bytes.Buffer
	if json.Indent(&p, v, "", "  ") != nil {
		return nil, false
	}
	p.WriteByte('\n')
	return p.Bytes(), true
}

// normalizeJSON compacts values that were shown pretty-printed.
func normalizeJSON(data map[string][]byte, pretty map[string]bool) map[string][]byte {
	for k := range pretty {
		v, ok := data[k]
		if !ok || !json.Valid(v) {
			continue
		}
		var c bytes.Buffer
		if json.Compact(&c, v) == nil {
			data[k] = c.Bytes()
		}
	}
	return data
}

// renderData renders values as YAML, using block scalars for multi-line text,
// pretty-printing compact JSON, and falling back to quoted style for any key whose
// block form would not round-trip. It returns the keys that were pretty-printed.
func renderData(data map[string][]byte) (string, map[string]bool, error) {
	pretty := map[string]bool{}
	if len(data) == 0 {
		return "", pretty, nil
	}
	shown := make(map[string][]byte, len(data))
	for k, v := range data {
		shown[k] = v
		if p, ok := prettyJSON(v); ok {
			shown[k] = p
			pretty[k] = true
		}
	}
	keys := sortedKeys(shown)
	literal := make(map[string]bool, len(keys))
	for _, k := range keys {
		literal[k] = true
	}

	for attempt := 0; attempt < 2; attempt++ {
		root := &yaml.Node{Kind: yaml.MappingNode}
		for _, k := range keys {
			root.Content = append(root.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				valueNode(shown[k], literal[k]))
		}
		out, err := yaml.Marshal(root)
		if err != nil {
			return "", nil, fmt.Errorf("failed to render secret: %w", err)
		}
		parsed, err := parseContent(out)
		if err != nil {
			return "", nil, fmt.Errorf("failed to render secret: %w", err)
		}
		ok := true
		for _, k := range keys {
			if !bytes.Equal(parsed[k], shown[k]) {
				literal[k] = false
				ok = false
			}
		}
		if ok {
			return string(out), pretty, nil
		}
	}
	return "", nil, fmt.Errorf("failed to render secret: values do not round-trip")
}

func parseContent(content []byte) (map[string][]byte, error) {
	var raw map[string]string
	if err := yaml.Unmarshal(content, &raw); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	result := make(map[string][]byte, len(raw))
	for k, v := range raw {
		result[k] = []byte(v)
	}
	return result, nil
}

const errorPrefix = "# ERROR: "

func withErrorHeader(content []byte, err error) string {
	body := string(content)
	if strings.HasPrefix(body, errorPrefix) {
		for strings.HasPrefix(body, errorPrefix) {
			_, body, _ = strings.Cut(body, "\n")
		}
		body = strings.TrimPrefix(body, "#\n")
	}

	var b strings.Builder
	for _, line := range strings.Split(err.Error(), "\n") {
		b.WriteString(errorPrefix + line + "\n")
	}
	b.WriteString("#\n")
	return b.String() + body
}

// mergeData replaces the shown subset of current with edited.
func mergeData(current, shown, edited map[string][]byte) map[string][]byte {
	out := copyData(current)
	for k := range shown {
		delete(out, k)
	}
	for k, v := range edited {
		out[k] = v
	}
	return out
}

type change struct {
	op       byte
	key      string
	old, new int
}

func diffData(before, after map[string][]byte) []change {
	var changes []change
	for _, k := range sortedKeys(before) {
		v, ok := after[k]
		switch {
		case !ok:
			changes = append(changes, change{op: '-', key: k, old: len(before[k])})
		case !bytes.Equal(v, before[k]):
			changes = append(changes, change{op: '~', key: k, old: len(before[k]), new: len(v)})
		}
	}
	for _, k := range sortedKeys(after) {
		if _, ok := before[k]; !ok {
			changes = append(changes, change{op: '+', key: k, new: len(after[k])})
		}
	}
	return changes
}

func printChanges(w io.Writer, changes []change, before, after map[string][]byte, showValues bool) {
	if len(changes) == 0 {
		fmt.Fprintln(w, "  (no keys)")
		return
	}
	width := 0
	for _, c := range changes {
		width = max(width, len(c.key))
	}
	for _, c := range changes {
		var detail string
		switch c.op {
		case '+':
			detail = fmt.Sprintf("(%d bytes)", c.new)
		case '-':
			detail = "(removed)"
		case '~':
			detail = fmt.Sprintf("(%d -> %d bytes)", c.old, c.new)
		}
		if c.op != '-' {
			if s := certSummary(after[c.key]); s != "" {
				detail += "  " + s
			}
		}
		fmt.Fprintf(w, "  %c %-*s  %s\n", c.op, width, c.key, detail)
		if showValues {
			if c.op != '+' {
				printValue(w, "-", before[c.key])
			}
			if c.op != '-' {
				printValue(w, "+", after[c.key])
			}
		}
	}
}

func printValue(w io.Writer, sign string, v []byte) {
	if !isText(v) {
		fmt.Fprintf(w, "      %s <binary, %d bytes>\n", sign, len(v))
		return
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(v), "\n"), "\n") {
		fmt.Fprintf(w, "      %s %s\n", sign, line)
	}
}

func applyFlagEdits(data map[string][]byte, sets, fromFiles, unsets []string, stdin io.Reader) (map[string][]byte, error) {
	out := copyData(data)
	for _, s := range sets {
		k, v, ok := strings.Cut(s, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("invalid --set %q, expected KEY=VALUE", s)
		}
		out[k] = []byte(v)
	}
	for _, f := range fromFiles {
		k, path, ok := strings.Cut(f, "=")
		if !ok {
			k, path = filepath.Base(f), f
		}
		if k == "" || path == "" || (path == "-" && !ok) {
			return nil, fmt.Errorf("invalid --from-file %q, expected [KEY=]PATH or KEY=-", f)
		}
		var v []byte
		var err error
		if path == "-" {
			v, err = io.ReadAll(stdin)
		} else {
			v, err = os.ReadFile(path)
		}
		if err != nil {
			return nil, fmt.Errorf("--from-file: %w", err)
		}
		out[k] = v
	}
	for _, k := range unsets {
		if _, ok := out[k]; !ok {
			return nil, fmt.Errorf("--unset: key %q not found", k)
		}
		delete(out, k)
	}
	return out, nil
}

func copyData(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func sortedKeys(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
