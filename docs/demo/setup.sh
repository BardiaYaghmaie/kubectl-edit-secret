#!/usr/bin/env bash
# Creates the demo fixtures used by demo.tape. Usage: KUBECONFIG=<kind kubeconfig> docs/demo/setup.sh
set -euo pipefail
ctx=$(kubectl config current-context)
[[ $ctx == kind-* || $ctx == staging ]] || { echo "refusing to run against $ctx" >&2; exit 1; }

ns=payments
kubectl delete ns $ns --ignore-not-found --wait=true >/dev/null
kubectl create ns $ns >/dev/null
k() { kubectl -n $ns "$@" >/dev/null; }

# Fake demo values for a throwaway kind cluster, not real credentials.
k create secret generic postgres-creds --from-literal=DB_HOST=postgres.payments.svc.cluster.local --from-literal=DB_USER=payments_app --from-literal='DB_PASSWORD=Sup3r-S3cret!' # ggignore
k create secret generic stripe-api --from-literal=STRIPE_KEY=sk_test_51Hd3m0K3yN0tR3al --from-literal=WEBHOOK_SECRET=whsec_d3m0 # ggignore
tmp=$(mktemp -d)
openssl req -x509 -newkey rsa:2048 -nodes -keyout "$tmp/k" -out "$tmp/c" -subj /CN=api.payments.example.com -days 90 2>/dev/null
k create secret tls api-tls --cert="$tmp/c" --key="$tmp/k"
rm -rf "$tmp"
k create secret docker-registry regcred --docker-server=ghcr.io --docker-username=ci-bot --docker-password=d3m0 # ggignore

for d in payments-api payments-worker; do
  k create deployment $d --image=registry.k8s.io/pause:3.9
  k set env deployment/$d --from=secret/postgres-creds
done
echo "demo fixtures ready in namespace $ns"
