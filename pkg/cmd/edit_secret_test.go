package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestOptions(t *testing.T, secret *corev1.Secret, stdin string) (*EditSecretOptions, *fake.Clientset, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	o := NewEditSecretOptions(genericclioptions.IOStreams{In: strings.NewReader(stdin), Out: out, ErrOut: out})
	client := fake.NewSimpleClientset()
	if secret != nil {
		client = fake.NewSimpleClientset(secret)
	}
	o.client = client
	o.namespace = "default"
	o.secretName = "s"
	o.isTerminal = func() bool { return true }
	return o, client, out
}

func testSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "default"},
		Data: map[string][]byte{
			"user": []byte("admin"),
			"pw":   []byte("old"),
			"bin":  {0x00, 0xff},
		},
	}
}

func getData(t *testing.T, o *EditSecretOptions) map[string][]byte {
	t.Helper()
	s, err := o.client.CoreV1().Secrets("default").Get(context.Background(), "s", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s.Data
}

// scriptEditor returns an editor command that replaces the file with content.
func scriptEditor(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "content")
	if err := os.WriteFile(src, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return "cp " + src
}

func TestRunEditorFlowPreservesBinary(t *testing.T) {
	o, _, out := newTestOptions(t, testSecret(), "y\n")
	o.editor = "sed -i s/old/new/"
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := getData(t, o)
	if string(d["pw"]) != "new" || string(d["user"]) != "admin" || !bytes.Equal(d["bin"], []byte{0x00, 0xff}) {
		t.Fatalf("unexpected data: %q", d)
	}
	if !strings.Contains(out.String(), "~ pw") || !strings.Contains(out.String(), "secret/s edited") {
		t.Fatalf("unexpected output: %s", out)
	}
}

func TestRunConfirmDeclined(t *testing.T) {
	o, _, out := newTestOptions(t, testSecret(), "n\n")
	o.editor = "sed -i s/old/new/"
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(getData(t, o)["pw"]) != "old" || !strings.Contains(out.String(), "Aborted") {
		t.Fatalf("expected abort, got %s", out)
	}
}

func TestRunNonTerminalRequiresYes(t *testing.T) {
	o, _, _ := newTestOptions(t, testSecret(), "")
	o.isTerminal = func() bool { return false }
	o.sets = []string{"pw=x"}
	if err := o.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected --yes error, got %v", err)
	}
	o.yes = true
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(getData(t, o)["pw"]) != "x" {
		t.Fatal("set not applied")
	}
}

func TestRunCancelledWhenUnchanged(t *testing.T) {
	o, _, out := newTestOptions(t, testSecret(), "")
	o.editor = "true"
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "cancelled") {
		t.Fatalf("got %s", out)
	}
}

func TestRunSingleKey(t *testing.T) {
	o, _, _ := newTestOptions(t, testSecret(), "")
	o.key = "pw"
	o.yes = true
	o.editor = scriptEditor(t, "pw: |\n  multi\n  line\n")
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := getData(t, o)
	if string(d["pw"]) != "multi\nline\n" || string(d["user"]) != "admin" || len(d) != 3 {
		t.Fatalf("unexpected data: %q", d)
	}
}

func TestRunInvalidYAMLReopensEditor(t *testing.T) {
	o, _, out := newTestOptions(t, testSecret(), "")
	o.yes = true
	// First run writes invalid YAML; second run sees the error header and fixes it.
	dir := t.TempDir()
	script := filepath.Join(dir, "ed.sh")
	body := `#!/bin/sh
if grep -q '^# ERROR:' "$1"; then
  printf 'pw: fixed\n' > "$1"
else
  printf 'pw: [broken\n' > "$1"
fi
`
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	o.editor = script
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := getData(t, o)
	if string(d["pw"]) != "fixed" || len(d) != 1 {
		t.Fatalf("unexpected data: %q", d)
	}
	if !strings.Contains(out.String(), "Reopening editor") {
		t.Fatalf("expected reopen, got %s", out)
	}
}

func TestRunNotFound(t *testing.T) {
	o, _, _ := newTestOptions(t, nil, "")
	o.sets = []string{"a=b"}
	if err := o.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "--create") {
		t.Fatalf("expected --create hint, got %v", err)
	}
}

func TestRunCreate(t *testing.T) {
	o, _, out := newTestOptions(t, nil, "")
	o.create = true
	o.yes = true
	o.secretType = "kubernetes.io/basic-auth"
	o.sets = []string{"username=u", "password=p"}
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err := o.client.CoreV1().Secrets("default").Get(context.Background(), "s", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != "kubernetes.io/basic-auth" || string(s.Data["password"]) != "p" {
		t.Fatalf("unexpected secret: %+v", s)
	}
	if !strings.Contains(out.String(), "secret/s created") {
		t.Fatalf("got %s", out)
	}
}

func TestRunNoChanges(t *testing.T) {
	o, _, out := newTestOptions(t, testSecret(), "")
	o.sets = []string{"pw=old"}
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "No changes") {
		t.Fatalf("got %s", out)
	}
}
