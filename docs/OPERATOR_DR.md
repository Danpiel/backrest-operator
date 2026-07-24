# Operator Disaster Recovery

This guide describes how to back up Backrest Operator control-plane state and recover after operator loss, cluster rebuild, or namespace deletion.

Manual restic/CSI procedures remain valid when the operator is completely unavailable — see [MANUAL_BACKUP.md](./MANUAL_BACKUP.md) and [MANUAL_RESTORE.md](./MANUAL_RESTORE.md).

## What to protect

| Asset | Location | Why it matters |
|-------|----------|----------------|
| Backrest host PVC | Bound to BackrestCluster host StatefulSet/Deployment | Plans, repo config, multihost pairing state, UI settings |
| `backrest-plans` ConfigMap | Backrest host namespace | Serialized BackupPlan fragments synced by the operator |
| Repository Secrets | App / `backrest-system` namespaces | `RESTIC_PASSWORD`, cloud credentials (`AWS_*`, `B2_*`, etc.) |
| Backrest auth Secret | Referenced by `BackrestCluster.spec.auth.existingSecret` | UI login credentials when auth is enabled |
| Custom Resources | All namespaces | Desired state — can be recreated from Git if you store manifests |
| Helm release values | Your GitOps repo | Operator/MCP/chart configuration |

Restic snapshot data in object storage is independent of the operator — protect repository credentials separately.

## Backup strategy

### 1. Back up the Backrest host PVC

Use your platform's volume backup (Velero, CSI snapshot + restic, or storage-array replication).

**CSI VolumeSnapshot example:**

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: backrest-host-dr
  namespace: backrest-system
spec:
  volumeSnapshotClassName: csi-hostpath-snapclass
  source:
    persistentVolumeClaimName: backrest-host-data   # adjust to your PVC name
```

Export the snapshot to durable storage outside the cluster if possible.

### 2. Export Secrets and ConfigMaps

```bash
NS=backrest-system

kubectl get secret -n "$NS" -o yaml \
  $(kubectl get secret -n "$NS" -o name | grep -E 'backup-repo|backrest-auth') \
  > backrest-secrets-backup.yaml

kubectl get configmap backrest-plans -n "$NS" -o yaml \
  > backrest-plans-backup.yaml
```

Store encrypted offline copies. Do not commit Secrets to Git.

### 3. Export Custom Resources

```bash
kubectl get backrestclusters,backuprepositories,backupplans,pvcbackups,pvcrestores \
  -A -o yaml > backrest-crs-backup.yaml
```

Prefer maintaining CR manifests in Git (see `examples/`) as the source of truth.

### 4. Optional — restic backup of operator state

Run a scheduled Job (or manual restic backup) that archives:

- `/data` from the Backrest host PVC mount
- Exported Secret/ConfigMap YAML (from a sidecar or init step)

Tag backups with `operator-state` for easy identification.

## Recovery procedure

### Scenario A — Operator pod failure (CRs and PVC intact)

1. Restart or upgrade the Helm release:

   ```bash
   helm upgrade --install backrest-operator ./charts/backrest-operator \
     --namespace backrest-system
   ```

2. Verify reconciliation:

   ```bash
   kubectl get backrestcluster,backuprepository -n backrest-system
   kubectl logs -n backrest-system deploy/backrest-operator
   ```

### Scenario B — Fresh cluster, same object-store repositories

1. Install the operator Helm chart.
2. Restore Secrets (`backup-repo`, env secrets, auth secret).
3. Restore or recreate the Backrest host PVC from snapshot.
4. Apply CR manifests (`BackrestCluster`, `BackupRepository`, `BackupPlan`).
5. Wait for Backrest host Ready and agent pairing:

   ```bash
   kubectl get backrestcluster main -n backrest-system -w
   ```

6. Trigger a test backup:

   ```bash
   kubectl apply -f examples/pvcbackup-csi.yaml
   ```

7. Verify restic snapshots still list correctly via `repo_status` MCP tool or `restic snapshots`.

### Scenario C — Total operator loss, repositories only in S3

1. Install operator on a new cluster.
2. Recreate Secrets with the **same** `RESTIC_PASSWORD` and backend credentials.
3. Create `BackupRepository` CRs pointing at the same restic URLs.
4. Use [MANUAL_RESTORE.md](./MANUAL_RESTORE.md) to recover application PVCs.
5. Rebuild Backrest plans from Git or re-create `BackupPlan` CRs.

### Scenario D — Backrest host PVC lost, restic repos intact

1. Install operator and create a new `BackrestCluster` (fresh host PVC).
2. Restore `BackupRepository` CRs and Secrets.
3. Re-apply `BackupPlan` CRs — the operator re-syncs plan fragments to the new host.
4. Re-pair agents (DaemonSet roll restart):

   ```bash
   kubectl rollout restart daemonset -n backrest-system -l app.kubernetes.io/component=agent
   ```

## Validation checklist

After recovery, confirm:

- [ ] Operator and MCP pods Running
- [ ] `BackrestCluster` status `Ready`
- [ ] `BackupRepository` phase `Ready`, verify CronJob scheduled (if enabled)
- [ ] Test `PVCBackup` succeeds
- [ ] Test `PVCRestore` or export download
- [ ] Prometheus metrics scraping (`backrest_operator_backup_total`)
- [ ] MCP auth works with a bound `backrest-operator` role

## RPO / RTO guidance

| Component | Typical RPO | Notes |
|-----------|-------------|-------|
| Application data (restic) | Plan schedule (hourly/daily) | Defined by BackupPlan/PVCBackup |
| Operator host PVC | Snapshot cadence | Daily snapshot recommended |
| CR manifests | Git commit | Near-zero if GitOps |
| Secrets | Secret backup job | Match credential rotation policy |

## Preventive practices

1. Store all CR manifests and Helm values in Git.
2. Use External Secrets or a secrets manager with documented restore steps.
3. Schedule VolumeSnapshots for the Backrest host PVC.
4. Periodically run `restic check` (enabled by default on repositories).
5. Test restore drills quarterly using [MANUAL_RESTORE.md](./MANUAL_RESTORE.md).

## Related docs

- [Operator usage](./USAGE.md)
- [Manual backup](./MANUAL_BACKUP.md)
- [Manual restore](./MANUAL_RESTORE.md)
