package mcp

import "encoding/json"

type ToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

func ToolSchemas() []ToolSchema {
	return []ToolSchema{
		toolSchema("list_clusters", "List BackrestCluster resources", `{"type":"object","properties":{"namespace":{"type":"string"}},"required":[]}`),
		toolSchema("get_cluster", "Get a BackrestCluster", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"}},"required":["namespace","name"]}`),
		toolSchema("get_repository", "Get BackupRepository", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"}},"required":["namespace","name"]}`),
		toolSchema("list_repositories", "List BackupRepositories", `{"type":"object","properties":{"namespace":{"type":"string"}},"required":["namespace"]}`),
		toolSchema("create_repository", "Create BackupRepository", `{"type":"object","properties":{"namespace":{"type":"string"},"body":{"type":"object"}},"required":["namespace","body"]}`),
		toolSchema("delete_repository", "Delete BackupRepository (destructive)", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"},"allow_destructive":{"type":"boolean"}},"required":["namespace","name"]}`),
		toolSchema("list_plans", "List BackupPlans", `{"type":"object","properties":{"namespace":{"type":"string"}},"required":["namespace"]}`),
		toolSchema("create_plan", "Create BackupPlan", `{"type":"object","properties":{"namespace":{"type":"string"},"body":{"type":"object"}},"required":["namespace","body"]}`),
		toolSchema("delete_plan", "Delete BackupPlan (destructive)", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"},"allow_destructive":{"type":"boolean"}},"required":["namespace","name"]}`),
		toolSchema("trigger_backup", "Force-run an existing PVCBackup (annotate force-run) or create a new one from body", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string","description":"Existing PVCBackup name to force-run"},"token":{"type":"string","description":"Optional unique force-run token"},"body":{"type":"object","description":"PVCBackup body when creating a new resource"}},"required":["namespace"]}`),
		toolSchema("create_pvc_restore", "Create PVCRestore", `{"type":"object","properties":{"namespace":{"type":"string"},"body":{"type":"object"}},"required":["namespace","body"]}`),
		toolSchema("get_pvc_restore", "Get PVCRestore status (includes exportURL)", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"}},"required":["namespace","name"]}`),
		toolSchema("restore_export", "Create export Job and return a download URL (waits until URL is ready; prefers public HTTPS when configured)", `{"type":"object","properties":{"namespace":{"type":"string"},"repository_name":{"type":"string"},"repository_namespace":{"type":"string"},"snapshot_id":{"type":"string"},"path_filters":{"type":"array","items":{"type":"string"}},"ttl_seconds":{"type":"integer"},"wait_seconds":{"type":"integer","description":"Max seconds to wait for exportURL (default 120)"}},"required":["namespace","repository_name"]}`),
		toolSchema("repo_status", "Repository status summary", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"}},"required":["namespace","name"]}`),
		toolSchema("list_snapshots", "List restic snapshots via Backrest host API for a synced BackupRepository", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"},"plan_id":{"type":"string"},"cluster_namespace":{"type":"string"},"cluster_name":{"type":"string"}},"required":["namespace","name"]}`),
		toolSchema("get_snapshot_download_url", "Get a signed Backrest URL that streams a snapshot path as .tar (no export Job). Uses GetDownloadURL on an indexed snapshot operation.", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string","description":"BackupRepository name"},"snapshot_id":{"type":"string"},"path":{"type":"string","description":"Path inside snapshot; default / (whole snapshot as .tar)"},"plan_id":{"type":"string"},"public_base_url":{"type":"string","description":"Optional HTTPS base (default BACKREST_PUBLIC_BASE_URL or https://backup.prq-infra.net)"},"cluster_namespace":{"type":"string"},"cluster_name":{"type":"string"}},"required":["namespace","name","snapshot_id"]}`),
		toolSchema("get_host_config", "Get Backrest host config (repos/plans; secrets redacted)", `{"type":"object","properties":{"cluster_namespace":{"type":"string"},"cluster_name":{"type":"string"}},"required":[]}`),
		toolSchema("index_repository", "Ask Backrest host to index snapshots for a repository", `{"type":"object","properties":{"namespace":{"type":"string"},"name":{"type":"string"},"cluster_namespace":{"type":"string"},"cluster_name":{"type":"string"}},"required":["namespace","name"]}`),
	}
}

func toolSchema(name, desc, input string) ToolSchema {
	return ToolSchema{Name: name, Description: desc, InputSchema: json.RawMessage(input)}
}
