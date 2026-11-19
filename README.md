# kubectl edit-secret

**Edit Kubernetes secrets like they are plain text files. Because they should be.**

[![Krew](https://img.shields.io/badge/kubectl-krew-brightgreen.svg)](https://krew.sigs.k8s.io/plugins/)
[![CI](https://github.com/BardiaYaghmaie/kubectl-edit-secret/actions/workflows/ci.yml/badge.svg?branch=main&event=push)](https://github.com/BardiaYaghmaie/kubectl-edit-secret/actions/workflows/ci.yml)
[![Go](https://img.shields.io/github/go-mod/go-version/BardiaYaghmaie/kubectl-edit-secret?color=brightgreen)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)

<p align="center">
  <img src="docs/demo.gif" alt="Rotating a database password with kubectl edit-secret: tab completion, editing in vim, a change summary, and restarting the workloads that use it" width="860">
</p>

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

- **It reads like a text file.** Certificates and configs as real multi-line blocks, JSON pretty-printed, binary values preserved byte-for-byte.
- **It's careful with production.** A change summary and a confirmation that names the cluster, TLS key/cert checks, and a warning when Helm, Argo CD or Flux manages the secret.
- **It doesn't lose your work.** Typos reopen the editor with the error on top; conflicts and permission errors save your edits to a file.
- **It finishes the rotation.** Shows which workloads use the secret, and `--restart` rolls them.
- **It doesn't leak.** Decoded values stay in RAM (`/dev/shm` on Linux) and are wiped on exit, Ctrl-C, `kill` or a dropped SSH session.

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

## Examples

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

The demo GIF is recorded with [vhs](https://github.com/charmbracelet/vhs) from `docs/demo/demo.tape` against a kind cluster (`docs/demo/setup.sh` creates the fixtures).

The end-to-end suite drives real editors (including `vi`), a pseudo-terminal for prompts, signals,
RBAC, TLS rotation, GitOps-managed secrets and shell completion.

Contributions welcome: open an issue or a pull request. See [CHANGELOG.md](CHANGELOG.md) for what's new.

## Like it?

If it saved you some base64 pain, a star on the repo helps other people find it. Feedback, bugs and ideas are just as welcome: open an [issue](https://github.com/BardiaYaghmaie/kubectl-edit-secret/issues) or join the [discussions](https://github.com/BardiaYaghmaie/kubectl-edit-secret/discussions).

## Related

- [kubectl-view-secret](https://github.com/elsesiy/kubectl-view-secret): view decoded secrets (read-only)
- [kubectl-modify-secret](https://github.com/rajatjindal/kubectl-modify-secret): similar idea

## License

MIT, see [LICENSE](LICENSE).
