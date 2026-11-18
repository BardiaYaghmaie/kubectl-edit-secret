package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	authv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func newTestOptions(t *testing.T, secret *corev1.Secret, stdin string) (*EditSecretOptions, *fake.Clientset, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	o := NewEditSecretOptions(genericclioptions.IOStreams{In: strings.NewReader(stdin), Out: out, ErrOut: out})
	client := fake.NewSimpleClientset()
	if secret != nil {
		client = fake.NewSimpleClientset(secret)
	}
	client.PrependReactor("create", "selfsubjectaccessreviews", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
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

func TestRunImmutable(t *testing.T) {
	s := testSecret()
	yes := true
	s.Immutable = &yes
	o, _, _ := newTestOptions(t, s, "")
	o.editor = "sed -i s/old/new/"
	if err := o.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("expected immutable error, got %v", err)
	}
}

func TestRunAccessDenied(t *testing.T) {
	o, client, _ := newTestOptions(t, testSecret(), "")
	client.PrependReactor("create", "selfsubjectaccessreviews", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{}, nil
	})
	o.sets = []string{"pw=x"}
	o.yes = true
	if err := o.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "not allowed to update") {
		t.Fatalf("expected access error, got %v", err)
	}
}

func TestRunManagedNeedsForce(t *testing.T) {
	s := testSecret()
	s.Labels = map[string]string{"app.kubernetes.io/managed-by": "Helm"}
	s.Annotations = map[string]string{lastAppliedAnnotation: `{"stringData":{"pw":"old"}}`, "keep": "me"}
	o, _, out := newTestOptions(t, s, "")
	o.sets = []string{"pw=x"}
	o.yes = true
	if err := o.Run(context.Background()); err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected --force error, got %v", err)
	}
	if !strings.Contains(out.String(), "managed by Helm") {
		t.Fatalf("no warning: %s", out)
	}
	o.force = true
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, _ := o.client.CoreV1().Secrets("default").Get(context.Background(), "s", metav1.GetOptions{})
	if _, ok := got.Annotations[lastAppliedAnnotation]; ok || got.Annotations["keep"] != "me" || got.Labels["app.kubernetes.io/managed-by"] != "Helm" {
		t.Fatalf("metadata: %v %v", got.Annotations, got.Labels)
	}
}

func TestRunForbiddenSavesEdits(t *testing.T) {
	o, client, _ := newTestOptions(t, testSecret(), "")
	client.PrependReactor("update", "secrets", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, apierrors.NewForbidden(schema.GroupResource{Resource: "secrets"}, "s", nil)
	})
	o.yes = true
	o.editor = "sed -i s/old/mine/"
	err := o.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "saved to") {
		t.Fatalf("expected saved edits, got %v", err)
	}
	path := strings.TrimSpace(strings.Split(strings.SplitN(err.Error(), "saved to:\n", 2)[1], "\n")[0])
	defer os.Remove(path)
	b, rerr := os.ReadFile(path)
	if rerr != nil || !strings.Contains(string(b), "pw: mine") {
		t.Fatalf("saved file: %q %v", b, rerr)
	}
}

func TestRunNonTerminalDoesNotLoopOnInvalid(t *testing.T) {
	o, _, _ := newTestOptions(t, testSecret(), "")
	o.isTerminal = func() bool { return false }
	o.yes = true
	o.editor = scriptEditor(t, "pw: [broken\n")
	err := o.Run(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid YAML") {
		t.Fatalf("expected invalid YAML error, got %v", err)
	}
	removeSaved(err)
}

func removeSaved(err error) {
	if err == nil {
		return
	}
	for _, line := range strings.Split(err.Error(), "\n") {
		if p := strings.TrimSpace(line); strings.HasPrefix(p, "/") {
			os.Remove(p)
		}
	}
}

func TestRunSingleKeyRestricted(t *testing.T) {
	o, _, _ := newTestOptions(t, testSecret(), "")
	o.key = "pw"
	o.yes = true
	o.isTerminal = func() bool { return false }
	o.editor = scriptEditor(t, "pw: x\nextra: y\n")
	err := o.Run(context.Background())
	removeSaved(err)
	if err == nil || !strings.Contains(err.Error(), "only key") {
		t.Fatalf("expected single-key error, got %v", err)
	}
}

func TestValidateTypes(t *testing.T) {
	o := NewEditSecretOptions(genericclioptions.IOStreams{})
	o.secretName = "s"
	o.typeChanged = true
	if err := o.Validate(); err == nil {
		t.Fatal("--type without --create should fail")
	}
	o.create = true
	o.secretType = "tls"
	if err := o.Validate(); err == nil || !strings.Contains(err.Error(), "unknown secret type") {
		t.Fatalf("typo type should fail: %v", err)
	}
	o.secretType = "example.com/custom"
	if err := o.Validate(); err != nil {
		t.Fatalf("custom domain type should pass: %v", err)
	}
}

func TestRunRestartConsumers(t *testing.T) {
	uses := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
		Spec: appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{
			Name: "c", EnvFrom: []corev1.EnvFromSource{{SecretRef: &corev1.SecretEnvSource{LocalObjectReference: corev1.LocalObjectReference{Name: "s"}}}},
		}}}}},
	}
	other := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}}
	o, _, out := newTestOptions(t, testSecret(), "")
	client := fake.NewSimpleClientset(testSecret(), uses, other)
	client.PrependReactor("create", "selfsubjectaccessreviews", func(k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authv1.SelfSubjectAccessReview{Status: authv1.SubjectAccessReviewStatus{Allowed: true}}, nil
	})
	o.client = client
	o.sets = []string{"pw=x"}
	o.yes = true
	o.restart = true
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Used by: deployment/api") || !strings.Contains(out.String(), "deployment/api restarted") || strings.Contains(out.String(), "other") {
		t.Fatalf("got %s", out)
	}
	d, _ := client.AppsV1().Deployments("default").Get(context.Background(), "api", metav1.GetOptions{})
	if d.Spec.Template.Annotations["kubectl.kubernetes.io/restartedAt"] == "" {
		t.Fatal("deployment not restarted")
	}
}

func TestRunCreateScaffold(t *testing.T) {
	o, _, _ := newTestOptions(t, nil, "")
	o.create = true
	o.yes = true
	o.secretType = "kubernetes.io/basic-auth"
	ed := filepath.Join(t.TempDir(), "ed.sh")
	if err := os.WriteFile(ed, []byte("#!/bin/sh\ngrep -q '^password: \"\"' \"$1\" && sed -i 's/^password: .*/password: p/; s/^username: .*/username: u/' \"$1\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	o.editor = ed
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	d := getData(t, o)
	if string(d["username"]) != "u" || string(d["password"]) != "p" {
		t.Fatalf("scaffold: %q", d)
	}
}

func TestRunEditorWithQuotedArgs(t *testing.T) {
	o, _, _ := newTestOptions(t, testSecret(), "")
	o.yes = true
	o.editor = `sed -i 's/old/with space/'`
	if err := o.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if string(getData(t, o)["pw"]) != "with space" {
		t.Fatalf("got %q", getData(t, o)["pw"])
	}
}
