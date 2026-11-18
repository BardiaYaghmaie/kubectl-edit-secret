# kubectl-edit-secret

A kubectl plugin to edit Kubernetes secrets with **decoded values**. No more manual base64 encoding/decoding!

[![Krew](https://img.shields.io/badge/kubectl-krew-blue.svg)](https://krew.sigs.k8s.io/plugins/)
[![Go Report Card](https://goreportcard.com/badge/github.com/BardiaYaghmaie/kubectl-edit-secret)](https://goreportcard.com/report/github.com/BardiaYaghmaie/kubectl-edit-secret)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Features

- **Decoded editing**: values shown in plain text, re-encoded on save
- **Multi-line friendly**: certs, PEM keys and config files shown as YAML block scalars (`|`)
- **Binary-safe**: non-text values shown as `!!binary` (base64) and preserved byte-for-byte
- **Diff + confirm**: see which keys are added/changed/removed before applying (`--yes` to skip)
- **Reopen on error**: invalid YAML or API validation errors reopen the editor with the error on top
- **Conflict detection**: if the secret changes while you edit, nothing is overwritten and your edits are saved to a file
- **Non-interactive**: `--set`, `--from-file`, `--unset` for scripts
- **`--create`**: create a secret if it doesn't exist (`--type` for TLS, basic-auth, ...)
- **`--dry-run`**: server-side validation without applying
- **Fuzzy picker**: omit the secret name to pick one with `fzf`
- **Tab completion**: secret names, keys, namespaces and contexts with rich descriptions; one-command setup
- **Secure temp files**: plaintext lives in a private `0700` dir (in `/dev/shm` on Linux, so never on disk) and is zeroed and removed afterwards
- **Full kubectl flag compatibility**: namespace, context, kubeconfig, ...

## Installation

### Via Krew (Recommended)

Install using [Krew](https://krew.sigs.k8s.io/), the kubectl plugin manager:

```bash
kubectl krew install edit-secret
```

### Via Go Install

```bash
go install github.com/BardiaYaghmaie/kubectl-edit-secret/cmd/kubectl-edit_secret@latest
```

Make sure `$(go env GOPATH)/bin` is in your PATH.

### Manual Build

```bash
# Clone the repository
git clone https://github.com/BardiaYaghmaie/kubectl-edit-secret.git
cd kubectl-edit-secret

# Build and install
make build
make install
# Or install globally (requires sudo)
make install-global
```

## Usage

### Basic Usage

```bash
# Edit all keys in a secret
kubectl edit-secret my-secret

# Edit a specific key
kubectl edit-secret my-secret password
```

### With Namespace

```bash
# Edit a secret in a specific namespace
kubectl edit-secret my-secret -n production

# Edit a specific key in a namespace
kubectl edit-secret db-credentials password -n database
```

### With Custom Editor

```bash
# Use nano instead of default editor
kubectl edit-secret my-secret --editor=nano

# Use VS Code (if configured to wait)
kubectl edit-secret my-secret --editor="code --wait"
```

### Non-interactive

```bash
kubectl edit-secret my-secret --set password=hunter2 --unset old-key --yes
kubectl edit-secret tls --from-file tls.crt=./cert.pem --from-file tls.key=./key.pem --yes
```

### Create, dry-run, picker

```bash
kubectl edit-secret new-secret --create
kubectl edit-secret creds --create --type kubernetes.io/basic-auth --set username=u --set password=p -y
kubectl edit-secret my-secret --set password=x --dry-run   # validated by the API server, not applied
kubectl edit-secret -n production                         # pick a secret with fzf
```

### Shell completion

One command:

```bash
kubectl edit-secret completion install
```

Then `kubectl edit-secret <TAB>` completes secret names, keys, namespaces and contexts, with descriptions in zsh and fish:

```
$ kubectl edit-secret -n prod <TAB>
db-creds  -- Opaque · 2 keys
web-tls   -- kubernetes.io/tls · 2 keys

$ kubectl edit-secret -n prod web-tls <TAB>
tls.crt  -- multi-line · 1164 bytes
tls.key  -- multi-line · 1704 bytes
```

Requires kubectl >= 1.26 with kubectl completion enabled in your shell. For the standalone binary:
`source <(kubectl-edit_secret completion zsh)` (also `bash`, `fish`, `powershell`).

### Example Workflow

1. Run the edit command:
   ```bash
   kubectl edit-secret my-app-secret
   ```

2. Your editor opens with decoded values:
   ```yaml
   # Editing secret: my-app-secret
   # Namespace: default
   #
   # Values are DECODED (plain text) and re-encoded on save.
   # Multi-line values use YAML block scalars (|). Binary values are tagged !!binary (base64).
   # Add, change or remove keys. Save and exit to apply; exit without saving to cancel.
   #
   api-key: my-super-secret-api-key
   ca.crt: |
     -----BEGIN CERTIFICATE-----
     MIIBszCCAVmgAwIBAgIU...
     -----END CERTIFICATE-----
   keystore.p12: !!binary MIIKXAIBAzCCChYGCSqGSIb3...
   ```

3. Save and exit. You get a summary and a confirmation prompt:
   ```
   Changes to secret/my-app-secret (namespace default):
     ~ api-key   (23 -> 31 bytes)
     + new-key   (5 bytes)
   Apply these changes? [y/N]:
   ```

## Comparison with `kubectl edit secret`

| Feature | `kubectl edit secret` | `kubectl edit-secret` |
|---------|----------------------|----------------------|
| View values | Base64 encoded | Plain text |
| Edit values | Manual base64 encode/decode | Plain text |
| Workflow | Copy → Decode → Edit → Encode → Paste | Just edit! |

## Configuration

### Editor Priority

The plugin looks for an editor in this order:

1. `--editor` flag
2. `$KUBE_EDITOR` environment variable
3. `$EDITOR` environment variable
4. System defaults: `vim`, `vi`, `nano` (`notepad` on Windows)

### Setting Default Editor

```bash
# In your shell profile (.bashrc, .zshrc, etc.)
export EDITOR=vim
# or
export KUBE_EDITOR=nano
```

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--editor` | `-e` | Editor to use |
| `--yes` | `-y` | Apply without confirmation |
| `--dry-run` | | Show changes and validate server-side without applying |
| `--create` | | Create the secret if it doesn't exist |
| `--type` | | Secret type when creating (default `Opaque`) |
| `--set KEY=VALUE` | | Set a key (repeatable, no editor) |
| `--from-file [KEY=]PATH` | | Set a key from a file (repeatable, no editor) |
| `--unset KEY` | | Remove a key (repeatable, no editor) |
| `--version` | | Print version |
| `--namespace` | `-n` | Kubernetes namespace |
| `--context` | | Kubernetes context |
| `--kubeconfig` | | Path to kubeconfig file |

All standard kubectl flags are supported.

## Building from Source

```bash
# Clone
git clone https://github.com/BardiaYaghmaie/kubectl-edit-secret.git
cd kubectl-edit-secret

# Download dependencies
make deps

# Build
make build

# Build for all platforms
make build-all

# Run unit tests
make test

# Run end-to-end tests against a kind cluster (refuses non-kind contexts)
make e2e
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

1. Fork the repository
2. Create your feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add some amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Related Projects

- [kubectl-view-secret](https://github.com/elsesiy/kubectl-view-secret) - View secrets decoded
- [kubectl-modify-secret](https://github.com/rajatjindal/kubectl-modify-secret) - Similar functionality

## Acknowledgments

- Inspired by [kubectl-view-secret](https://github.com/elsesiy/kubectl-view-secret)
- Built with [cli-runtime](https://github.com/kubernetes/cli-runtime)
