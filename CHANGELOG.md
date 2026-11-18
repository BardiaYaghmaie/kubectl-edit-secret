# Changelog

All notable changes to this project are documented here. Format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); versions follow [SemVer](https://semver.org/).

## [0.2.0] - 2026-11-18

The "safe at 3am" release.

### Added
- **Tab completion** for secret names, keys, namespaces, contexts, `--unset` and `--type`, with
  descriptions (secret type and key count; whether a key is text, multi-line, a certificate or binary).
  One-command setup: `kubectl edit-secret completion install`. Standalone scripts via
  `completion bash|zsh|fish|powershell`.
- **Change summary and confirmation** before anything is applied, showing the target context.
  `--yes` / `-y` skips the prompt; `--show-values` prints old and new values.
- **Reopen on error**: invalid YAML or an API validation error reopens the editor with the error on top.
- **Your edits are never lost**: on a conflict, a permission error, or any other failure they are saved to a private file.
- **Workload awareness**: shows which Deployments, StatefulSets and DaemonSets use the secret;
  `--restart` rolls them after the change.
- **TLS checks**: a mismatched `tls.crt`/`tls.key` or an expired certificate is caught before applying,
  and the summary shows the new certificate's CN, SANs and expiry.
- **GitOps guard**: warns when a secret is managed by Helm, Argo CD, Flux or an owner controller,
  since your change may be reverted. With `--yes`, `--force` is required to proceed.
- **Readable values**: multi-line values as YAML block scalars (`|`), JSON values (like `.dockerconfigjson`)
  pretty-printed for editing and compacted back on save.
- `--create` with `--type`; the editor opens with the fields that type needs (e.g. `tls.crt`/`tls.key`).
- `--dry-run` (server-side validation without applying).
- `--set KEY=VALUE`, `--from-file [KEY=]PATH`, `--from-file KEY=-` (stdin, keeps values out of shell history), `--unset KEY`.
- Interactive **fzf picker** when no secret name is given; a table of secrets otherwise.
- Permission check before the editor opens, so you don't edit something you cannot save.
- `--version`, unit tests, a 29-check end-to-end suite on kind (`make e2e`), CI, and automated releases to krew.

### Fixed
- **Binary values are no longer corrupted.** Non-text values are shown as `!!binary` and preserved byte-for-byte.
- Lines starting with `#` inside values (e.g. config files) are no longer dropped.
- Editor commands with quotes or spaces (`KUBE_EDITOR="code --wait"`, paths with spaces) now work: the editor runs through the shell, like `kubectl edit`.
- A change to labels or annotations by a controller while you edit no longer causes a conflict; the edit is retried automatically.

### Security
- Decoded values live in a private `0700` directory, in `/dev/shm` on Linux so they never touch disk.
  They are wiped on exit, on Ctrl-C, SIGTERM, SIGHUP and SIGPIPE, along with editor swap files.
  Leftovers from a killed process are swept on the next run.
- The `kubectl.kubernetes.io/last-applied-configuration` annotation is removed on edit, since it can hold the previous values in plain text.
- Immutable secrets fail fast instead of opening an editor that cannot save.
- Release workflow: actions pinned to commit SHAs, least-privilege token permissions, Dependabot updates.

### Changed
- Applying without a terminal now requires `--yes`. Editor mode without a terminal fails on invalid edits instead of reopening.
- `--type` without `--create` and unknown secret types (e.g. `tls` instead of `kubernetes.io/tls`) are rejected; `--force` allows custom types.

## [0.1.1] - 2025-12-03

### Changed
- Reduced cyclomatic complexity and fixed gofmt issues.

## [0.1.0] - 2025-12-03

### Added
- Initial release: edit Kubernetes secrets with decoded values in your editor.

[0.2.0]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/compare/v0.1.1...v0.2.0
[0.1.1]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/BardiaYaghmaie/kubectl-edit-secret/releases/tag/v0.1.0
