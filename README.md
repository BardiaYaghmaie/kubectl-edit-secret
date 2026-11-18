# kubectl edit-secret

**Edit Kubernetes secrets like they are plain text files. Because they should be.**

[![Krew](https://img.shields.io/badge/kubectl-krew-blue.svg)](https://krew.sigs.k8s.io/plugins/)
[![CI](https://github.com/BardiaYaghmaie/kubectl-edit-secret/actions/workflows/ci.yml/badge.svg)](https://github.com/BardiaYaghmaie/kubectl-edit-secret/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/BardiaYaghmaie/kubectl-edit-secret)](https://goreportcard.com/report/github.com/BardiaYaghmaie/kubectl-edit-secret)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

```bash
kubectl krew install edit-secret
kubectl edit-secret completion install   # optional: tab completion
```

`kubectl edit secret` shows you this:

```yaml
data:
  password: aHVudGVyMg==
  tls.crt: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUJzekNDQVZtZ0F3SUJBZ0lVYzBk...
```

`kubectl edit-secret` shows you this:

```yaml
# Editing secret: web
# Namespace: production
# Context: prod-eu
password: hunter2
tls.crt: |
  -----BEGIN CERTIFICATE-----
  MIIBszCCAVmgAwIBAgIUc0d...
  -----END CERTIFICATE-----
```

Edit, save, quit. It re-encodes, shows you what changed, and asks before touching the cluster:

```
Changes to secret/web (namespace production, context prod-eu):
  ~ password  (7 -> 12 bytes)
  ~ tls.crt   (1164 -> 1170 bytes)  CN=web.example.com, SANs=web.example.com, expires 2027-11-18 (365 days)
Used by: deployment/web, deployment/worker
Apply these changes and restart the workloads above to context prod-eu? [y/N]:
```

No more `echo ... | base64 -d`, copy, paste, `base64 -w0`, paste back, and hoping you didn't add a newline.

## Why you'll like it

**It's an editor, done right**
- Values in plain text. Certificates and config files as readable multi-line blocks. JSON (hello, `.dockerconfigjson`) pretty-printed for editing and compacted back on save.
- Binary values (keystores, `.p12`) are shown as `!!binary` and preserved byte-for-byte. Edit the rest around them safely.
- Made a typo? Invalid YAML or a rejected key reopens the editor with the error at the top. Your work is still there.
- Uses `$KUBE_EDITOR` / `$EDITOR` exactly like `kubectl edit`, including `code --wait`.

**It's safe in production**
- Every change shows a summary and asks for confirmation, with the **context name in the prompt** so you know which cluster you are about to change.
- Validates TLS secrets before applying: a key that doesn't match the certificate, or an expired certificate, is caught **before** your pods restart with it.
- Warns when a secret is managed by **Helm, Argo CD, Flux or an operator**, because your change will probably be reverted on the next sync.
- Checks your permissions **before** opening the editor. If something still goes wrong (a conflict, a webhook, a network blip) your edits are saved to a file, never lost.
- If someone else changes the secret while you are editing, nothing is overwritten. If they only touched labels, your edit is retried automatically.
- Decoded values live in a private directory in **`/dev/shm`** on Linux (RAM, never disk) and are wiped on exit, Ctrl-C, `kill` or a dropped SSH session.
- Removes the stale `last-applied-configuration` annotation, which can hold your old values in plain text.

**It finishes the job**
- Shows which Deployments, StatefulSets and DaemonSets use the secret. `--restart` rolls them for you, because a rotation isn't done until the pods have the new value.
- Tab completion for secrets, keys, namespaces and contexts, with descriptions.
- No name? Pick one with **fzf**.
- Scriptable: `--set`, `--from-file`, `--unset`, `--dry-run`, `--yes`.

## Tour

```bash
# Edit a whole secret, or one key
kubectl edit-secret db-credentials
kubectl edit-secret db-credentials password

# Don't remember the name? Pick it (needs fzf)
kubectl edit-secret -n production

# Rotate a certificate and restart everything that uses it
kubectl edit-secret web-tls \
  --from-file tls.crt=./cert.pem --from-file tls.key=./key.pem --restart

# Rotate a password without it landing in your shell history
pwgen -s 32 1 | tr -d '\n' | kubectl edit-secret db-credentials --from-file password=- --yes

# Create secrets; the editor opens with the fields the type needs
kubectl edit-secret web-tls --create --type kubernetes.io/tls
kubectl edit-secret api --create --set token=abc123 --yes

# See exactly what would change, validated by the API server, without applying
kubectl edit-secret db-credentials --set password=x --dry-run --show-values

# In CI: no prompts, fail loudly on anything suspicious
kubectl edit-secret app-config --set FEATURE_X=true --yes
```

## Tab completion

```bash
kubectl edit-secret completion install
```

That's it (needs kubectl 1.26+ with kubectl completion enabled in your shell). Then:

```
$ kubectl edit-secret -n production <TAB>
db-credentials  -- Opaque · 2 keys
regcred         -- kubernetes.io/dockerconfigjson · 1 key
web-tls         -- kubernetes.io/tls · 2 keys

$ kubectl edit-secret -n production web-tls <TAB>
tls.crt  -- certificate
tls.key  -- multi-line
```

Using the binary directly? `source <(kubectl-edit_secret completion zsh)` (also `bash`, `fish`, `powershell`).

## Flags

| Flag | Description |
|------|-------------|
| `-e, --editor` | Editor to use (default: `$KUBE_EDITOR`, `$EDITOR`, then vim/vi/nano) |
| `-y, --yes` | Apply without the confirmation prompt |
| `--force` | With `--yes`: apply even with warnings (GitOps-managed secret, TLS validation) |
| `--dry-run` | Show changes and validate them server-side without applying |
| `--show-values` | Show old and new values in the change summary |
| `--restart` | Rollout-restart the workloads that use the secret |
| `--create` | Create the secret if it doesn't exist |
| `--type` | Secret type when creating (`kubernetes.io/tls`, `kubernetes.io/basic-auth`, ...) |
| `--set KEY=VALUE` | Set a key without opening the editor (repeatable) |
| `--from-file [KEY=]PATH` | Set a key from a file; `KEY=-` reads stdin (repeatable) |
| `--unset KEY` | Remove a key (repeatable) |
| `--version` | Print the version |

Plus every standard kubectl flag: `-n`, `--context`, `--kubeconfig`, `--as`, ...

## How it behaves

| Situation | What happens |
|-----------|--------------|
| You quit without saving | Nothing changes |
| You save invalid YAML / a rejected key | The editor reopens with the error on top |
| You answer `N` at the prompt | Nothing changes |
| Someone else changed the data meanwhile | Nothing is overwritten; your edits are saved to a file |
| A controller only touched labels meanwhile | Your edit is retried and applied |
| You lack permission to update | You are told before the editor opens |
| The secret is immutable | You are told immediately |
| No terminal (CI) | Requires `--yes`; invalid edits fail instead of reopening |
| The secret is managed by Helm/Argo/Flux | Warning; with `--yes` you need `--force` |

Exit code is non-zero only when something failed. "No changes", "cancelled" and "aborted" exit 0.

## Install

```bash
# Krew (recommended)
kubectl krew install edit-secret

# Go
go install github.com/BardiaYaghmaie/kubectl-edit-secret/cmd/kubectl-edit_secret@latest

# From source
git clone https://github.com/BardiaYaghmaie/kubectl-edit-secret.git
cd kubectl-edit-secret && make install
```

## Development

```bash
make test   # unit tests
make e2e    # 29 end-to-end checks against a kind cluster (refuses to run on non-kind contexts)
```

The end-to-end suite drives real editors (including `vi`), a pseudo-terminal for prompts, signals,
RBAC, TLS rotation, GitOps-managed secrets and shell completion.

Contributions welcome: open an issue or a pull request. See [CHANGELOG.md](CHANGELOG.md) for what's new.

## Related

- [kubectl-view-secret](https://github.com/elsesiy/kubectl-view-secret): view decoded secrets (read-only)
- [kubectl-modify-secret](https://github.com/rajatjindal/kubectl-modify-secret): similar idea

## License

MIT, see [LICENSE](LICENSE).
