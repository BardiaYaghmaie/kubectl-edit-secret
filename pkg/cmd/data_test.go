package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenderParseRoundTrip(t *testing.T) {
	data := map[string][]byte{
		"plain":       []byte("hunter2"),
		"empty":       {},
		"multiline":   []byte("-----BEGIN CERT-----\nabc\n-----END CERT-----\n"),
		"no-trailing": []byte("line1\nline2"),
		"hash-lines":  []byte("# not a comment\nkey=value\n"),
		"lead-space":  []byte("  indented\nnext\n"),
		"trail-space": []byte("a  \nb \n"),
		"number":      []byte("5432"),
		"bool":        []byte("true"),
		"binary":      {0x00, 0xff, 0xfe, 0x01},
		"latin1":      {'c', 'a', 'f', 0xe9},
		"unicode":     []byte("سلام"),
		"tabs":        []byte("a\tb"),
		"colon":       []byte("key: value"),
		"nbsp":        []byte("a\u00a0b"),
		"emoji":       []byte("p\u00e4ss\U0001F511"),
	}
	out, _, err := renderData(data)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseContent([]byte(out))
	if err != nil {
		t.Fatalf("parse: %v\n%s", err, out)
	}
	if len(parsed) != len(data) {
		t.Fatalf("got %d keys, want %d", len(parsed), len(data))
	}
	for k, v := range data {
		if !bytes.Equal(parsed[k], v) {
			t.Errorf("%s: got %q, want %q", k, parsed[k], v)
		}
	}
	if !strings.Contains(out, "multiline: |") {
		t.Errorf("expected block scalar for multiline:\n%s", out)
	}
	if strings.Contains(out, "nbsp: !!binary") {
		t.Errorf("non-breaking space should be text:\n%s", out)
	}
	if !strings.Contains(out, "binary: !!binary") {
		t.Errorf("expected !!binary tag:\n%s", out)
	}
}

func TestParseContentErrors(t *testing.T) {
	for _, in := range []string{"a: [1, 2]", "a: {b: c}", "a: 1\na: 2", "a: \"unterminated"} {
		if _, err := parseContent([]byte(in)); err == nil {
			t.Errorf("expected error for %q", in)
		}
	}
}

func TestParseContentEmpty(t *testing.T) {
	got, err := parseContent([]byte("# only comments\n"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v", got, err)
	}
}

func TestWithErrorHeaderReplacesPrevious(t *testing.T) {
	first := withErrorHeader([]byte("a: b\n"), errors.New("boom"))
	second := withErrorHeader([]byte(first), errors.New("bang\nline2"))
	want := "# ERROR: bang\n# ERROR: line2\n#\na: b\n"
	if second != want {
		t.Fatalf("got %q, want %q", second, want)
	}
}

func TestMergeData(t *testing.T) {
	current := map[string][]byte{"a": []byte("1"), "b": []byte("2"), "c": []byte("3")}
	got := mergeData(current, map[string][]byte{"b": []byte("2")}, map[string][]byte{"b2": []byte("x")})
	if _, ok := got["b"]; ok || string(got["b2"]) != "x" || string(got["a"]) != "1" || string(got["c"]) != "3" {
		t.Fatalf("unexpected merge: %v", got)
	}
	if _, ok := current["b2"]; ok {
		t.Fatal("mergeData mutated input")
	}
}

func TestDiffData(t *testing.T) {
	before := map[string][]byte{"keep": []byte("x"), "gone": []byte("y"), "mod": []byte("old")}
	after := map[string][]byte{"keep": []byte("x"), "mod": []byte("newer"), "add": []byte("z")}
	got := diffData(before, after)
	want := []change{{op: '-', key: "gone", old: 1}, {op: '~', key: "mod", old: 3, new: 5}, {op: '+', key: "add", new: 1}}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("change %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestApplyFlagEdits(t *testing.T) {
	dir := t.TempDir()
	cert := filepath.Join(dir, "tls.crt")
	if err := os.WriteFile(cert, []byte("CERT"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := applyFlagEdits(
		map[string][]byte{"old": []byte("1"), "pw": []byte("a")},
		[]string{"pw=b=c", "empty="},
		[]string{cert, "renamed=" + cert, "stdin=-"},
		[]string{"old"},
		strings.NewReader("from-stdin"),
	)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"pw": "b=c", "empty": "", "tls.crt": "CERT", "renamed": "CERT", "stdin": "from-stdin"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if string(got[k]) != v {
			t.Errorf("%s: got %q, want %q", k, got[k], v)
		}
	}

	for _, tc := range []struct{ sets, files, unsets []string }{
		{sets: []string{"novalue"}},
		{sets: []string{"=x"}},
		{files: []string{"k=" + filepath.Join(dir, "missing")}},
		{unsets: []string{"nope"}},
		{files: []string{"-"}},
	} {
		if _, err := applyFlagEdits(map[string][]byte{}, tc.sets, tc.files, tc.unsets, strings.NewReader("")); err == nil {
			t.Errorf("expected error for %+v", tc)
		}
	}
}

func TestWorkspaceWipes(t *testing.T) {
	ws, err := newWorkspace()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(ws.dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("workspace perms: %v %v", info.Mode(), err)
	}
	if err := os.WriteFile(ws.Path("s.yaml"), []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	ws.Close()
	if _, err := os.Stat(ws.dir); !os.IsNotExist(err) {
		t.Fatalf("workspace not removed: %v", err)
	}
}

func TestJSONPrettyRoundTrip(t *testing.T) {
	compact := []byte(`{"auths":{"r.io":{"auth":"eA=="}}}`)
	data := map[string][]byte{".dockerconfigjson": compact, "spaced": []byte(`{"a": 1}`)}
	out, pretty, err := renderData(data)
	if err != nil {
		t.Fatal(err)
	}
	if !pretty[".dockerconfigjson"] || pretty["spaced"] {
		t.Fatalf("pretty keys: %v", pretty)
	}
	if !strings.Contains(out, "\n      \"auths\": {") {
		t.Fatalf("not pretty-printed:\n%s", out)
	}
	parsed, err := parseContent([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	got := normalizeJSON(parsed, pretty)
	if !bytes.Equal(got[".dockerconfigjson"], compact) || string(got["spaced"]) != `{"a": 1}` {
		t.Fatalf("round trip: %q", got)
	}
}
