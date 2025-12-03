# kubectl-edit-secret

A kubectl plugin to edit Kubernetes secrets with **decoded values**. No more manual base64 encoding/decoding!

[![Go Report Card](https://goreportcard.com/badge/github.com/BardiaYaghmaie/kubectl-edit-secret)](https://goreportcard.com/report/github.com/BardiaYaghmaie/kubectl-edit-secret)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

## Features

- 🔓 **Automatic Decoding**: View and edit secret values in plain text
- 🔐 **Automatic Encoding**: Changes are base64-encoded before applying
- ✏️ **Editor Support**: Uses your preferred editor (`$KUBE_EDITOR`, `$EDITOR`, or `--editor`)
- 🎯 **Selective Editing**: Edit all keys or just a specific key
- 🔧 **Full kubectl Compatibility**: Supports namespace, context, kubeconfig flags

## Installation

### Via Krew (Recommended)

```bash
kubectl krew install edit-secret
```

### Manual Installation

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

### From Source

```bash
go install github.com/BardiaYaghmaie/kubectl-edit-secret/cmd/kubectl-edit_secret@latest
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
   # Modify the values below. Lines starting with '#' are ignored.
   # The values shown are DECODED (plain text).
   # They will be automatically base64-encoded when saved.
   #
   # Save and exit to apply changes. Exit without saving to cancel.
   #
   api-key: my-super-secret-api-key
   database-url: postgres://user:password@localhost:5432/mydb
   jwt-secret: another-secret-value
   ```

3. Make your changes, save, and exit. The secret is automatically updated!

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
4. System defaults: `vim`, `vi`, `nano`

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
| `--editor` | `-e` | Editor to use for editing |
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

# Run tests
make test
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

