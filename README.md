# Backrest Operator

Kubernetes operator for [Backrest](https://github.com/garethgeorge/backrest) (restic backup orchestrator), with a built-in **MCP server** so AI agents can manage backup plans and restores under Kubernetes RBAC.

| | |
|---|---|
| API group | `operator.backrest.io/v1alpha1` |
| Operator | Go (controller-runtime) |
| MCP | Go (HTTP JSON-RPC + stdio) |
| Backrest image | `ghcr.io/garethgeorge/backrest:v1.14.1` |
| License | Apache-2.0 |
| Spec | [SPEC.md](./SPEC.md) |

## Quickstart

```bash
# Install operator + MCP + CRDs
helm upgrade --install backrest-operator ./charts/backrest-operator \
  --namespace backrest-system \
  --create-namespace

# Create secrets, cluster, repository, and a test backup
kubectl apply -f examples/auth-secret.yaml
kubectl apply -f examples/backrestcluster.yaml
kubectl apply -f examples/backuprepository-s3.yaml
kubectl apply -f examples/pvcbackup-csi.yaml

# Watch backup progress
kubectl get pvcbackup -A -w
```

Bind your user to a shipped ClusterRole before using MCP — see [examples/rbac-binding.yaml](./examples/rbac-binding.yaml).

## Documentation

| Doc | Description |
|-----|-------------|
| [docs/USAGE.md](./docs/USAGE.md) | Helm install, CRs, CSI/TopoLVM, append-only, verify disable |
| [docs/MCP.md](./docs/MCP.md) | HTTP Bearer token, stdio mode, RBAC roles, destructive flag |
| [docs/MANUAL_BACKUP.md](./docs/MANUAL_BACKUP.md) | VolumeSnapshot + restic without the operator |
| [docs/MANUAL_RESTORE.md](./docs/MANUAL_RESTORE.md) | Restore to PVC or local tar without the operator |
| [docs/OPERATOR_DR.md](./docs/OPERATOR_DR.md) | Backrest host PVC + secrets backup and DR recovery |

## Examples

Generic manifests in [examples/](./examples/):

- `backrestcluster.yaml` — Backrest host + agents
- `backuprepository-s3.yaml` — S3 restic repository with scheduled verify
- `backupplan.yaml` — Scheduled plan with retention
- `pvcbackup-csi.yaml` / `pvcbackup-quiesced.yaml` — PVC backup strategies
- `pvcrestore-existing.yaml` / `pvcrestore-export.yaml` — Restore modes
- `ingress-ui-https.yaml` — HTTPS Ingress (cert-manager + Traefik) + HTTP redirect
- `oauth2-proxy.yaml` — oauth2-proxy reverse-proxy in front of the UI
- `auth-secret.yaml`, `externalsecret.yaml`, `rbac-binding.yaml`

## Development

```bash
go test ./...
go build -o bin/operator ./cmd/operator
go build -o bin/mcp ./cmd/mcp
make docker
```

## Features

- Backrest host + agents (multihost), optional Ingress and UI auth
- CRDs for repositories, plans, PVC backup/restore
- Backup modes: live flush, quiesced copy, CSI and TopoLVM snapshots
- All restic/Backrest storage backends; append-only switch; scheduled `restic check`
- Restore to existing/new PVC, partial paths, export download (curl / MCP)
- Separate MCP Deployment (HTTP + stdio) with TokenReview / SAR / impersonation
- Prometheus & VictoriaMetrics scrapes, embedded alerts, Grafana dashboard
- Validating webhooks for CRDs and AI-shaped requests

## License

Apache License 2.0 — see [LICENSE](./LICENSE). You may use, modify, and redistribute under a different project name.
