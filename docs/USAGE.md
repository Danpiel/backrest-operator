# Backrest Operator — Usage Guide

This guide covers installing the Helm chart and creating the core Custom Resources (CRs) for `operator.backrest.io/v1alpha1`.

## Prerequisites

- Kubernetes 1.27+ with a CSI snapshot controller (for snapshot-based backups)
- Helm 3
- A restic-compatible object store or filesystem backend
- Optional: [TopoLVM](https://github.com/topolvm/topolvm) when using `topolvmSnapshot` in the backup pipeline

Default Backrest image pin: `ghcr.io/garethgeorge/backrest:v1.14.1`

## Install the operator

```bash
helm upgrade --install backrest-operator ./charts/backrest-operator \
  --namespace backrest-system \
  --create-namespace
```

Verify the operator and MCP pods are running:

```bash
kubectl get pods -n backrest-system
kubectl get crd | grep operator.backrest.io
```

### Watch filters (optional)

Limit which namespaces the operator reconciles by setting `operator.watch.namespaces` in Helm values:

```yaml
operator:
  watch:
    namespaces:
      - backrest-system
      - app-prod
```

An empty list watches all namespaces.

## 1. Create a BackrestCluster

Deploy the Backrest host, agents (multihost mode), and optional UI ingress:

```bash
kubectl apply -f examples/backrestcluster.yaml
kubectl get backrestcluster -n backrest-system
```

The host persists configuration on a PVC. Agents connect to the host Service on port `9898`.

See [examples/backrestcluster.yaml](../examples/backrestcluster.yaml).

## 2. Create a BackupRepository

Define where restic stores snapshots. Credentials live in Kubernetes Secrets — never inline in the CR.

```bash
kubectl apply -f examples/auth-secret.yaml
kubectl apply -f examples/backuprepository-s3.yaml
kubectl get backuprepository -n backrest-system
```

### Append-only repositories

Set `spec.appendOnly: true` to configure restic append-only mode. In append-only mode, forget/prune operations that rewrite history are rejected. Destructive MCP tools and deletes require admin RBAC and explicit flags.

### Scheduled `restic check` (verify)

By default, `spec.verify.enabled` is `true`. The operator schedules a CronJob that runs `restic check` on the cadence in `spec.verify.schedule` (default: weekly).

Disable verification when you manage checks externally:

```yaml
spec:
  verify:
    enabled: false
```

See [examples/backuprepository-s3.yaml](../examples/backuprepository-s3.yaml).

## 3. Create a BackupPlan

BackupPlans define scheduled paths, retention, hooks, and tags. The operator syncs plan fragments into the Backrest host ConfigMap.

```bash
kubectl apply -f examples/backupplan.yaml
kubectl get backupplan -n app
```

An empty `spec.schedule` means on-demand backups only (via PVCBackup or MCP `trigger_backup`).

See [examples/backupplan.yaml](../examples/backupplan.yaml).

## 4. Back up a PVC

Create a `PVCBackup` CR to orchestrate flush, quiesce, snapshot, and restic upload steps.

### CSI snapshot pipeline

Requires a `VolumeSnapshotClass` and CSI snapshot support:

```bash
kubectl apply -f examples/pvcbackup-csi.yaml
kubectl get pvcbackup -n app -w
```

### Quiesced live copy

Scale workloads down (or delete pods) before copying files:

```bash
kubectl apply -f examples/pvcbackup-quiesced.yaml
```

**Important:** `spec.quiesce.leaveDown: true` keeps workloads stopped after backup. This requires the confirmation annotation `operator.backrest.io/leave-down-confirmed: "true"` on the CR metadata.

### Pipeline steps

`spec.strategy.pipeline` is an ordered list. Supported values:

| Step | Description |
|------|-------------|
| `liveFlush` | Run flush command/script before backup |
| `csiSnapshot` | Create a CSI VolumeSnapshot |
| `topolvmSnapshot` | TopoLVM-specific snapshot (requires TopoLVM) |
| `quiescedLive` | Quiesce workloads and copy live files |

You can combine steps (for example, snapshot then upload from the snapshot mount).

## 5. Restore a PVC

### Restore to an existing PVC

```bash
kubectl apply -f examples/pvcrestore-existing.yaml
kubectl get pvcrestore -n app -w
```

### Export archive (curl / local download)

Creates a short-lived restore-proxy Job with a TTL-bound download URL:

```bash
kubectl apply -f examples/pvcrestore-export.yaml
kubectl get pvcrestore export-restore -n app -o jsonpath='{.status.exportURL}'
```

Export TTL must be between 60 and 86400 seconds (`spec.export.ttlSeconds`).

## 6. RBAC for human users

Bind users or groups to shipped ClusterRoles:

| Role | ClusterRole name | Capabilities |
|------|------------------|--------------|
| Read-only | `backrest-viewer` | get/list/watch CRs and status |
| Operator | `backrest-operator` | create/update backups, plans, restores |
| Admin | `backrest-admin` | delete, secrets, destructive operations |

See [examples/rbac-binding.yaml](../examples/rbac-binding.yaml).

## 7. External Secrets (optional)

If you use [External Secrets Operator](https://external-secrets.io/), sync repository credentials into the Secret referenced by `passwordSecretRef`. See the commented example in [examples/externalsecret.yaml](../examples/externalsecret.yaml).

## Troubleshooting

```bash
# Operator logs
kubectl logs -n backrest-system deploy/backrest-operator -f

# PVCBackup status
kubectl describe pvcbackup -n app <name>

# Webhook rejections
kubectl get validatingwebhookconfiguration
```

## Related docs

- [MCP server usage](./MCP.md)
- [Manual backup without operator](./MANUAL_BACKUP.md)
- [Manual restore without operator](./MANUAL_RESTORE.md)
- [Operator disaster recovery](./OPERATOR_DR.md)
