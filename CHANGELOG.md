# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/).

## [0.2.0] - 2026-11-18

### Added
- **Tab completion** for secret names, keys, `--namespace`, `--context`, `--unset` and `--type`, with
  descriptions (secret type and key count, key size, binary/multi-line). Set up with
  `kubectl edit-secret completion install`; standalone scripts via `completion bash|zsh|fish|powershell`.
- **Diff and confirmation**: a summary of added/changed/removed keys and a `[y/N]` prompt before applying. `--yes` / `-y` skips it.
- **Reopen on error**: invalid YAML or an API validation error reopens the editor with the error at the top, keeping your edits.
- **Conflict detection**: if the secret changes while you edit, nothing is overwritten and your edits are saved to a file.
- **Multi-line values** (certificates, keys, config files) are shown as YAML block scalars (`|`).
- `--create` and `--type` to create a secret that does not exist yet.
- `--dry-run` to preview changes and validate them server-side without applying.
- `--set KEY=VALUE`, `--from-file [KEY=]PATH` and `--unset KEY` for non-interactive edits.
- Interactive **fzf picker** when no secret name is given.
- `--version`.
- Unit tests, a kind-based end-to-end suite (`make e2e`), CI, and automated releases to krew.

### Fixed
- **Binary values are no longer corrupted.** Non-text values are shown as `!!binary` and preserved byte-for-byte.
- Lines starting with `#` inside multi-line values (e.g. config files) are no longer dropped.

### Security
- Decoded values are written to a private `0700` directory, in `/dev/shm` on Linux so they never touch disk,
  and are zeroed and removed afterwards, along with editor swap and backup files.

### Changed
- Applying edits without a terminal now requires `--yes`.

## [0.1.1] - 2025-12-03

### Changed
- Reduced cyclomatic complexity and fixed gofmt issues.

## [0.1.0] - 2025-12-03

### Added
- Initial release: edit Kubernetes secrets with decoded values in your editor.

[0.2.0]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/releases/tag/v0.1.0
