# Manual Backup Runbook (Without the Operator)

> **Day-2 path:** use a `PVCBackup` CR. This document is for operator-unavailable / emergency / educational use only.

Use this runbook when the Backrest Operator is unavailable, you need an emergency backup, or you want to understand what the operator automates.

This procedure combines a **CSI VolumeSnapshot** (point-in-time block copy) with a **restic backup** (deduplicated off-cluster storage).

## Prerequisites

- `kubectl`, `restic` CLI (or use the `restic/restic:0.19.1` container image)
- CSI VolumeSnapshot support and a VolumeSnapshotClass
- Repository password and backend credentials in Secrets or environment variables
- Read access to the target PVC namespace

## Overview

```
 PVC ──► VolumeSnapshot ──► temporary mount ──► restic backup ──► remote repository
```

## Step 1 — Create a VolumeSnapshot

Replace namespace, PVC name, and snapshot class:

```yaml
apiVersion: snapshot.storage.k8s.io/v1
kind: VolumeSnapshot
metadata:
  name: manual-snap-app-data
  namespace: app
spec:
  volumeSnapshotClassName: csi-hostpath-snapclass
  source:
    persistentVolumeClaimName: app-data
```

```bash
kubectl apply -f volumesnapshot.yaml
kubectl wait volumesnapshot/manual-snap-app-data -n app --for=jsonpath='{.status.readyToUse}'=true --timeout=300s
```

For **TopoLVM**, use your cluster's TopoLVM VolumeSnapshotClass and ensure the snapshot lands on the correct node if your driver requires node affinity for restore jobs.

## Step 2 — Mount the snapshot in a Job

Create a short-lived Pod or Job that mounts the snapshot as a volume. Example Pod spec fragment:

```yaml
volumes:
  - name: data
    persistentVolumeClaim:
      claimName: manual-snap-app-data  # bound snapshot PVC if your driver creates one
containers:
  - name: restic
    image: restic/restic:0.19.1
    env:
      - name: RESTIC_PASSWORD
        valueFrom:
          secretKeyRef:
            name: backup-repo
            key: RESTIC_PASSWORD
      - name: AWS_ACCESS_KEY_ID
        valueFrom:
          secretKeyRef:
            name: backup-repo-env
            key: AWS_ACCESS_KEY_ID
      - name: AWS_SECRET_ACCESS_KEY
        valueFrom:
          secretKeyRef:
            name: backup-repo-env
            key: AWS_SECRET_ACCESS_KEY
    command:
      - restic
    args:
      - backup
      - /data
      - --repo
      - s3:https://s3.example.com/my-bucket/backups
      - --tag
      - manual
      - --host
      - app-manual
    volumeMounts:
      - name: data
        mountPath: /data
        readOnly: true
```

> **Note:** Some CSI drivers expose snapshot content via a dynamically provisioned PVC; others require `volumeSnapshotContent` inspection. Adapt mount paths to your storage driver.

## Step 3 — Run restic backup

Exec into the Job or run locally with port-forwarded mount:

```bash
export RESTIC_PASSWORD="$(kubectl get secret backup-repo -n backrest-system -o jsonpath='{.data.RESTIC_PASSWORD}' | base64 -d)"

restic -r s3:https://s3.example.com/my-bucket/backups backup /mnt/data \
  --tag manual \
  --host app-manual-$(date +%Y%m%d)
```

## Step 4 — Verify

```bash
restic -r s3:https://s3.example.com/my-bucket/backups snapshots
restic -r s3:https://s3.example.com/my-bucket/backups check
```

## Step 5 — Clean up

```bash
kubectl delete volumesnapshot manual-snap-app-data -n app
kubectl delete job manual-restic-backup -n app
```

## Quiesce before snapshot (optional)

For application-consistent backups without the operator's quiesce controller:

1. Scale Deployments to zero or run application-specific flush commands.
2. Create the VolumeSnapshot.
3. Scale workloads back up.

Document application-specific flush steps in your runbook.

## Append-only repositories

If the repository uses restic append-only mode, omit forget/prune commands. Only `backup` and `check` are safe.

## What the operator adds

The operator automates quiesce/unquiesce, snapshot creation, restic Job scheduling, retention, metrics, and status on `PVCBackup` CRs. See [USAGE.md](./USAGE.md).
