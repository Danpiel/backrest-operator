# Manual Restore Runbook (Without the Operator)

Use this runbook to restore data from a restic repository when the Backrest Operator is down or you need a one-off recovery.

Two common paths:

1. **Restore to a PVC** — recover in-cluster data
2. **Restore to local tar** — download an archive for inspection or off-cluster recovery

## Prerequisites

- `kubectl`, `restic` CLI (or `restic/restic:0.19.1` image)
- Repository URL, password, and backend credentials
- A target PVC (existing or newly created) with sufficient capacity
- Snapshot ID from `restic snapshots` (or use `latest`)

## List available snapshots

```bash
export RESTIC_PASSWORD="$(kubectl get secret backup-repo -n backrest-system -o jsonpath='{.data.RESTIC_PASSWORD}' | base64 -d)"

restic -r s3:https://s3.example.com/my-bucket/backups snapshots
```

Note the snapshot short ID (first 8 characters) or full ID.

---

## Restore to an existing PVC

### Step 1 — Quiesce consumers (recommended)

Scale workloads using the PVC to zero or delete pods so files are not open during restore:

```bash
kubectl scale deployment/my-app -n app --replicas=0
kubectl wait --for=delete pod -l app=my-app -n app --timeout=120s
```

### Step 2 — Run a restore Job

Mount the target PVC read-write in a Job:

```yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: manual-restore-app-data
  namespace: app
spec:
  ttlSecondsAfterFinished: 3600
  template:
    spec:
      restartPolicy: Never
      containers:
        - name: restic
          image: restic/restic:0.19.1
          env:
            - name: RESTIC_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: backup-repo
                  key: RESTIC_PASSWORD
          command:
            - restic
          args:
            - restore
            - latest
            - --target
            - /data
            - --repo
            - s3:https://s3.example.com/my-bucket/backups
          volumeMounts:
            - name: data
              mountPath: /data
      volumes:
        - name: data
          persistentVolumeClaim:
            claimName: app-data
```

```bash
kubectl apply -f restore-job.yaml
kubectl logs -n app job/manual-restore-app-data -f
```

Replace `latest` with a snapshot ID for point-in-time recovery.

### Step 3 — Partial restore (optional)

Restore specific paths only:

```bash
restic restore abcd1234 --target /data --repo s3:https://s3.example.com/my-bucket/backups \
  --include /var/lib/myapp/db
```

### Step 4 — Bring workloads back

```bash
kubectl scale deployment/my-app -n app --replicas=1
```

---

## Restore to a new PVC

Create an empty PVC, then run the same Job with the new claim name:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: app-data-restored
  namespace: app
spec:
  accessModes:
    - ReadWriteOnce
  storageClassName: standard
  resources:
    requests:
      storage: 50Gi
```

Point the Job volume at `app-data-restored`.

---

## Restore to local tar (off-cluster)

Use `restic dump` to stream an archive without mounting a PVC locally.

### On your workstation

```bash
export RESTIC_PASSWORD="your-password"

restic -r s3:https://s3.example.com/my-bucket/backups dump latest / \
  | tar -xvf - -C ./restore-out
```

Dump a single path:

```bash
restic -r s3:https://s3.example.com/my-bucket/backups dump latest /var/lib/myapp \
  > myapp-data.tar
```

### Download via Backrest GetDownloadURL (preferred)

Prefer the operator/Backrest signed download path documented in [USAGE.md](./USAGE.md)
(`GetDownloadURL` + `/download/<jwt>/` Ingress). That streams a snapshot as `.tar`
without a custom export Job or `/export-proxy` binary.

---

## Restore from VolumeSnapshot (without restic)

If you only need block-level rollback and have a CSI snapshot:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: source-snap
  namespace: app
spec:
  volumeSnapshotClassName: csi-hostpath-snapclass
  source:
    persistentVolumeClaimName: app-data
```

Create a new PVC from the snapshot per your CSI driver's documentation, then attach to a Pod.

---

## Troubleshooting

| Symptom | Check |
|---------|-------|
| `repository not found` | Verify URL, credentials, and bucket path |
| `wrong password` | Confirm Secret key matches `RESTIC_PASSWORD` used at backup time |
| Permission denied on PVC | Job must run as user allowed to write mount path |
| Partial files | Use `--include` / path filters; verify snapshot ID |

## What the operator adds

The operator implements quiesce, restore Jobs, export URLs with TTL, and status on `PVCRestore` CRs. See [USAGE.md](./USAGE.md).
