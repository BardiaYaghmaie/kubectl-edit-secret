#!/usr/bin/env bash
# End-to-end tests against a kind cluster. Usage: KUBECONFIG=... test/e2e.sh
set -euo pipefail

ctx=$(kubectl config current-context)
[[ $ctx == kind-* ]] || { echo "refusing to run against non-kind context: $ctx" >&2; exit 1; }

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
ns=e2e-edit-secret-$$
trap 'kubectl delete ns "$ns" --wait=false >/dev/null 2>&1 || true; rm -rf "$work"' EXIT

go build -o "$work/bin/kubectl-edit_secret" "$root/cmd/kubectl-edit_secret"
export PATH="$work/bin:$PATH"
unset KUBE_EDITOR EDITOR

kubectl create ns "$ns" >/dev/null
k() { kubectl -n "$ns" "$@"; }
es() { kubectl edit-secret -n "$ns" "$@"; }
get() { k get secret "$1" -o "jsonpath={.data.$2}" | base64 -d; }
pass=0
ok() { echo "PASS: $*"; pass=$((pass + 1)); }
fail() { echo "FAIL: $*" >&2; exit 1; }
editor() { printf '#!/bin/sh\n%s\n' "$2" > "$work/$1"; chmod +x "$work/$1"; echo "$work/$1"; }

head -c 256 /dev/urandom > "$work/blob.bin"
printf -- '-----BEGIN CERTIFICATE-----\nMIIBszCCAVmgAwIBAgIU\n-----END CERTIFICATE-----\n' > "$work/cert.pem"
k create secret generic basic --from-literal=user=admin --from-literal=pw=old \
  --from-file=blob="$work/blob.bin" --from-file=tls.crt="$work/cert.pem" >/dev/null
blobsum=$(k get secret basic -o jsonpath='{.data.blob}' | base64 -d | sha256sum)

# --version
kubectl edit-secret --version | grep >/dev/null 'edit-secret version' && ok "--version"

# Rendering: block scalars, !!binary, private workspace in /dev/shm, wiped afterwards
ed=$(editor peek "cp \"\$1\" $work/peek.yaml; echo \"\$1\" > $work/peek.path")
es basic -e "$ed" | grep >/dev/null "cancelled" || fail "unchanged edit should cancel"
grep >/dev/null 'tls.crt: |' "$work/peek.yaml" || fail "no block scalar"
grep >/dev/null 'blob: !!binary' "$work/peek.yaml" || fail "no !!binary"
path=$(cat "$work/peek.path")
[[ $path == /dev/shm/* ]] || fail "workspace not in /dev/shm: $path"
[[ ! -e $path && ! -e $(dirname "$path") ]] || fail "workspace not wiped"
ok "render (block scalar, !!binary, /dev/shm, wipe, cancel on no-op)"

# Editor flow keeps binary intact
es basic -y -e "sed -i s/old/new/" >/dev/null
[[ $(get basic pw) == new ]] || fail "pw not updated"
[[ $(k get secret basic -o jsonpath='{.data.blob}' | base64 -d | sha256sum) == "$blobsum" ]] || fail "binary corrupted"
ok "editor flow, binary preserved"

# Real vi, driven by ex commands
if command -v vi >/dev/null; then
  es basic -y -e "vi -n -u NONE -c %s/new/vimmed/ -c wq" >/dev/null 2>&1
  [[ $(get basic pw) == vimmed ]] || fail "vi edit"
  es basic -y -e "vi -n -u NONE -c %s/vimmed/new/ -c wq" >/dev/null 2>&1
  ok "real vi"
fi

# Multi-line edit
ed=$(editor ml "sed -i 's/MIIBszCCAVmgAwIBAgIU/CHANGED/' \"\$1\"")
es basic -y -e "$ed" >/dev/null
get basic 'tls\.crt' | grep >/dev/null CHANGED || fail "multi-line edit"
[[ $(get basic 'tls\.crt' | wc -l) -eq 3 ]] || fail "multi-line shape lost"
ok "multi-line block edit"

# Single key
es basic user -y -e "sed -i s/admin/root/" >/dev/null
[[ $(get basic user) == root && $(get basic pw) == new ]] || fail "single key"
ok "single key edit"

# Invalid YAML reopens editor with error header
ed=$(editor fixyaml "if grep >/dev/null '^# ERROR:' \"\$1\"; then sed -i 's/^pw: .*/pw: fixed/' \"\$1\"; else sed -i 's/^pw: .*/pw: [broken/' \"\$1\"; fi")
es basic -y -e "$ed" 2>&1 | grep >/dev/null "Reopening editor" || fail "no reopen on invalid yaml"
[[ $(get basic pw) == fixed ]] || fail "fix after reopen"
ok "invalid YAML reopens editor"

# API validation error (bad key name) reopens editor
ed=$(editor badkey "if grep >/dev/null '^# ERROR:' \"\$1\"; then sed -i 's/^bad key!: /goodkey: /' \"\$1\"; else echo 'bad key!: x' >> \"\$1\"; fi")
es basic -y -e "$ed" 2>&1 | grep >/dev/null "Reopening editor" || fail "no reopen on API error"
[[ $(get basic goodkey) == x ]] || fail "fix after API error"
ok "API validation error reopens editor"

# Conflict: someone else updates the secret while editing
ed=$(editor conflict "kubectl -n $ns patch secret basic -p '{\"stringData\":{\"other\":\"1\"}}' >/dev/null; sed -i s/fixed/mine/ \"\$1\"")
out=$(es basic -y -e "$ed" 2>&1 || true)
echo "$out" | grep >/dev/null "modified by someone else" || fail "conflict message: $out"
saved=$(echo "$out" | grep -o '/[^ ]*kubectl-edit-secret-basic-[^ ]*\.yaml')
grep >/dev/null 'pw: mine' "$saved" || fail "edits not saved on conflict"
rm -f "$saved"
[[ $(get basic pw) == fixed ]] || fail "conflict should not apply"
ok "conflict detected, edits saved"

# --set / --from-file / --unset
es basic --set pw=s3t --from-file key.pem="$work/cert.pem" --unset goodkey -y >/dev/null
[[ $(get basic pw) == s3t ]] && get basic 'key\.pem' | grep >/dev/null BEGIN || fail "--set/--from-file"
[[ -z $(k get secret basic -o jsonpath='{.data.goodkey}') ]] || fail "--unset"
ok "--set / --from-file / --unset"

# No tty, no --yes => refuse
out=$(es basic --set pw=nope </dev/null 2>&1 || true)
echo "$out" | grep >/dev/null -- "--yes" || fail "should require --yes: $out"
[[ $(get basic pw) == s3t ]] || fail "applied without confirmation"
ok "non-tty requires --yes"

# Confirmation prompt on a tty
echo n | script -qec "kubectl edit-secret -n $ns basic --set pw=declined" /dev/null | grep >/dev/null Aborted || fail "decline"
[[ $(get basic pw) == s3t ]] || fail "applied after decline"
echo y | script -qec "kubectl edit-secret -n $ns basic --set pw=accepted" /dev/null | grep >/dev/null "edited" || fail "accept"
[[ $(get basic pw) == accepted ]] || fail "not applied after accept"
ok "confirmation prompt (y/N)"

# --dry-run validates server-side but does not apply
es basic --set pw=dry --dry-run | grep >/dev/null "server dry run" || fail "dry-run output"
[[ $(get basic pw) == accepted ]] || fail "dry-run applied"
out=$(es basic --set 'bad key=1' --dry-run 2>&1 || true)
echo "$out" | grep -i >/dev/null "invalid" || fail "dry-run should hit server validation: $out"
ok "--dry-run (server-side)"

# Not found / --create
out=$(es nope --set a=b -y 2>&1 || true); echo "$out" | grep >/dev/null -- "--create" || fail "not-found hint"
es created --create --type kubernetes.io/basic-auth --set username=u --set password=p -y | grep >/dev/null "created" || fail "create"
[[ $(k get secret created -o jsonpath='{.type}') == kubernetes.io/basic-auth && $(get created password) == p ]] || fail "create content"
ed=$(editor newsecret "printf 'token: abc\n' >> \"\$1\"")
es viaeditor --create -y -e "$ed" >/dev/null
[[ $(get viaeditor token) == abc ]] || fail "create via editor"
ok "--create (flags and editor)"

# Completion: standalone, with descriptions, and via kubectl plugin completion
c() { kubectl-edit_secret __complete "$@" 2>/dev/null; }
c -n "$ns" "" | grep -x "basic	Opaque · [0-9]* keys" >/dev/null || fail "complete secret names with description"
! c -n "$ns" "" | grep -E "^(completion|help)" >/dev/null || fail "subcommands leak into completion"
c -n "$ns" basic "" | grep -x "user	5 bytes\|user	4 bytes" >/dev/null || fail "complete keys"
c -n "$ns" basic "" | grep "^blob	binary" >/dev/null || fail "binary key description"
c -n "$ns" basic "" | grep "^tls.crt	multi-line" >/dev/null || fail "multi-line key description"
c -n "$ns" basic --unset "" | grep "^pw	" >/dev/null || fail "complete --unset keys"
c -n "" | grep -x "$ns" >/dev/null || fail "complete namespaces"
c --context "" | grep -x "$ctx" >/dev/null || fail "complete contexts"
rm -f "$work/bin/kubectl_complete-edit_secret"
kubectl edit-secret completion install | grep "$work/bin/kubectl_complete-edit_secret" >/dev/null || fail "completion install"
kubectl __complete edit-secret -n "$ns" "" 2>/dev/null | grep "^basic	" >/dev/null || fail "kubectl plugin completion"
kubectl-edit_secret completion zsh | grep "#compdef kubectl-edit_secret" >/dev/null || fail "zsh script"
kubectl-edit_secret completion bash | grep "kubectl-edit_secret" >/dev/null || fail "bash script"
ok "shell completion (descriptions, keys, namespaces, contexts, install, kubectl plugin, scripts)"

# fzf picker when no name is given
if command -v fzf >/dev/null; then
  FZF_DEFAULT_OPTS='--filter=^created$' script -qec "kubectl edit-secret -n $ns -e true" /dev/null | grep >/dev/null cancelled || fail "fzf picker"
  ok "fzf picker"
fi
out=$(es -e true </dev/null 2>&1 || true); echo "$out" | grep >/dev/null "basic" || fail "list secrets without fzf/tty"
ok "list secrets when no name and no tty"

echo "All $pass e2e checks passed."
