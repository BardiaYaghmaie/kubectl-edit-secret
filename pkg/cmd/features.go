package cmd

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var knownSecretTypes = []string{
	string(corev1.SecretTypeOpaque),
	string(corev1.SecretTypeTLS),
	string(corev1.SecretTypeDockerConfigJson),
	string(corev1.SecretTypeBasicAuth),
	string(corev1.SecretTypeSSHAuth),
	string(corev1.SecretTypeBootstrapToken),
}

func knownSecretType(t string) bool {
	for _, k := range knownSecretTypes {
		if t == k {
			return true
		}
	}
	return strings.Contains(t, "/") && !strings.HasPrefix(t, "kubernetes.io/")
}

// scaffold returns the keys a new secret of the given type needs, so the editor opens ready to fill.
func scaffold(t corev1.SecretType) map[string][]byte {
	switch t {
	case corev1.SecretTypeTLS:
		return map[string][]byte{"tls.crt": {}, "tls.key": {}}
	case corev1.SecretTypeBasicAuth:
		return map[string][]byte{"username": {}, "password": {}}
	case corev1.SecretTypeSSHAuth:
		return map[string][]byte{"ssh-privatekey": {}}
	case corev1.SecretTypeDockerConfigJson:
		return map[string][]byte{".dockerconfigjson": []byte(`{"auths":{"registry.example.com":{"username":"","password":""}}}`)}
	}
	return map[string][]byte{}
}

func managedWarnings(s *corev1.Secret) []string {
	var w []string
	revert := "your change may be reverted on the next sync"
	if v := s.Labels["app.kubernetes.io/managed-by"]; v != "" {
		w = append(w, fmt.Sprintf("managed by %s; %s", v, revert))
	}
	for _, r := range s.OwnerReferences {
		w = append(w, fmt.Sprintf("owned by %s/%s; its controller may overwrite your change", r.Kind, r.Name))
	}
	if s.Annotations["argocd.argoproj.io/tracking-id"] != "" || s.Labels["argocd.argoproj.io/instance"] != "" {
		w = append(w, "tracked by Argo CD; "+revert)
	}
	if s.Labels["kustomize.toolkit.fluxcd.io/name"] != "" || s.Labels["helm.toolkit.fluxcd.io/name"] != "" {
		w = append(w, "managed by Flux; "+revert)
	}
	return w
}

// validateData checks changed keys for common mistakes before anything is applied.
func validateData(t corev1.SecretType, changes []change, data map[string][]byte) []string {
	changed := map[string]bool{}
	for _, c := range changes {
		if c.op != '-' {
			changed[c.key] = true
		}
	}
	var problems []string

	crt, key := data[corev1.TLSCertKey], data[corev1.TLSPrivateKeyKey]
	if (changed[corev1.TLSCertKey] || changed[corev1.TLSPrivateKeyKey]) && (t == corev1.SecretTypeTLS || (len(crt) > 0 && len(key) > 0)) {
		if _, err := tls.X509KeyPair(crt, key); err != nil {
			problems = append(problems, fmt.Sprintf("tls.crt/tls.key: %v", err))
		}
	}
	for k := range changed {
		v := data[k]
		if c := parseCert(v); c != nil && time.Now().After(c.NotAfter) {
			problems = append(problems, fmt.Sprintf("%s: certificate expired on %s", k, c.NotAfter.Format("2006-01-02")))
		}
		if (k == corev1.DockerConfigJsonKey || strings.HasSuffix(k, ".json")) && !json.Valid(v) {
			problems = append(problems, fmt.Sprintf("%s: not valid JSON", k))
		}
	}
	return problems
}

func parseCert(v []byte) *x509.Certificate {
	block, _ := pem.Decode(v)
	if block == nil || block.Type != "CERTIFICATE" {
		return nil
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil
	}
	return c
}

func certSummary(v []byte) string {
	c := parseCert(v)
	if c == nil {
		return ""
	}
	s := "CN=" + c.Subject.CommonName
	if len(c.DNSNames) > 0 {
		s += ", SANs=" + strings.Join(c.DNSNames, ",")
	}
	days := int(time.Until(c.NotAfter).Hours() / 24)
	return fmt.Sprintf("%s, expires %s (%d days)", s, c.NotAfter.Format("2006-01-02"), days)
}

type consumer struct{ kind, name string }

func podSpecUses(spec corev1.PodSpec, name string) bool {
	for _, s := range spec.ImagePullSecrets {
		if s.Name == name {
			return true
		}
	}
	for _, v := range spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == name {
			return true
		}
		if v.Projected != nil {
			for _, src := range v.Projected.Sources {
				if src.Secret != nil && src.Secret.Name == name {
					return true
				}
			}
		}
	}
	containers := append(append([]corev1.Container{}, spec.InitContainers...), spec.Containers...)
	for _, c := range containers {
		for _, e := range c.EnvFrom {
			if e.SecretRef != nil && e.SecretRef.Name == name {
				return true
			}
		}
		for _, e := range c.Env {
			if e.ValueFrom != nil && e.ValueFrom.SecretKeyRef != nil && e.ValueFrom.SecretKeyRef.Name == name {
				return true
			}
		}
	}
	return false
}

// consumers finds workloads using the secret. Errors (e.g. no list permission) are ignored.
func (o *EditSecretOptions) consumers(ctx context.Context) []consumer {
	apps := o.client.AppsV1()
	var out []consumer
	if l, err := apps.Deployments(o.namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range l.Items {
			if podSpecUses(d.Spec.Template.Spec, o.secretName) {
				out = append(out, consumer{"deployment", d.Name})
			}
		}
	}
	if l, err := apps.StatefulSets(o.namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, s := range l.Items {
			if podSpecUses(s.Spec.Template.Spec, o.secretName) {
				out = append(out, consumer{"statefulset", s.Name})
			}
		}
	}
	if l, err := apps.DaemonSets(o.namespace).List(ctx, metav1.ListOptions{}); err == nil {
		for _, d := range l.Items {
			if podSpecUses(d.Spec.Template.Spec, o.secretName) {
				out = append(out, consumer{"daemonset", d.Name})
			}
		}
	}
	return out
}

func consumerNames(cs []consumer) []string {
	names := make([]string, len(cs))
	for i, c := range cs {
		names[i] = c.kind + "/" + c.name
	}
	return names
}

func (o *EditSecretOptions) restartConsumers(ctx context.Context, cs []consumer) error {
	if len(cs) == 0 {
		fmt.Fprintln(o.streams.Out, "No Deployments, StatefulSets or DaemonSets use this secret; nothing to restart.")
		return nil
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"template":{"metadata":{"annotations":{"kubectl.kubernetes.io/restartedAt":%q}}}}}`, time.Now().Format(time.RFC3339)))
	apps := o.client.AppsV1()
	var failed []string
	for _, c := range cs {
		var err error
		switch c.kind {
		case "deployment":
			_, err = apps.Deployments(o.namespace).Patch(ctx, c.name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		case "statefulset":
			_, err = apps.StatefulSets(o.namespace).Patch(ctx, c.name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		case "daemonset":
			_, err = apps.DaemonSets(o.namespace).Patch(ctx, c.name, types.StrategicMergePatchType, patch, metav1.PatchOptions{})
		}
		if err != nil {
			failed = append(failed, fmt.Sprintf("%s/%s: %v", c.kind, c.name, err))
			continue
		}
		fmt.Fprintf(o.streams.Out, "%s/%s restarted\n", c.kind, c.name)
	}
	if len(failed) > 0 {
		return fmt.Errorf("secret updated, but some restarts failed:\n  %s", strings.Join(failed, "\n  "))
	}
	return nil
}
