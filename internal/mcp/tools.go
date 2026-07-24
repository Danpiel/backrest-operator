package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"

	"github.com/Danpiel/backrest-operator/internal/backrest"
)

var (
	gvrCluster = schema.GroupVersionResource{Group: "operator.backrest.io", Version: "v1alpha1", Resource: "backrestclusters"}
	gvrRepo    = schema.GroupVersionResource{Group: "operator.backrest.io", Version: "v1alpha1", Resource: "backuprepositories"}
	gvrPlan    = schema.GroupVersionResource{Group: "operator.backrest.io", Version: "v1alpha1", Resource: "backupplans"}
	gvrBackup  = schema.GroupVersionResource{Group: "operator.backrest.io", Version: "v1alpha1", Resource: "pvcbackups"}
	gvrRestore = schema.GroupVersionResource{Group: "operator.backrest.io", Version: "v1alpha1", Resource: "pvcrestores"}
)

type Tools struct {
	base *rest.Config
}

func NewTools(cfg *rest.Config) *Tools {
	return &Tools{base: cfg}
}

func (t *Tools) clientFor(user *UserIdentity) (dynamic.Interface, error) {
	cfg := t.base
	if user != nil && user.Username != StdioUsername {
		cfg = ImpersonateConfig(t.base, user)
	}
	return dynamic.NewForConfig(cfg)
}

func (t *Tools) Call(ctx context.Context, user *UserIdentity, name string, args map[string]interface{}) (interface{}, error) {
	ns := strArg(args, "namespace", "default")
	allow := boolArg(args, "allow_destructive")
	dc, err := t.clientFor(user)
	if err != nil {
		return nil, err
	}

	switch name {
	case "list_clusters":
		listNS := strArg(args, "namespace", "")
		if listNS == "" {
			list, err := dc.Resource(gvrCluster).List(ctx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return list.Items, nil
		}
		list, err := dc.Resource(gvrCluster).Namespace(listNS).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	case "get_cluster":
		return dc.Resource(gvrCluster).Namespace(ns).Get(ctx, strArg(args, "name", ""), metav1.GetOptions{})
	case "list_repositories":
		list, err := dc.Resource(gvrRepo).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	case "get_repository":
		return dc.Resource(gvrRepo).Namespace(ns).Get(ctx, strArg(args, "name", ""), metav1.GetOptions{})
	case "create_repository":
		obj, err := bodyToUnstructured(args["body"], "BackupRepository", ns)
		if err != nil {
			return nil, err
		}
		return dc.Resource(gvrRepo).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	case "delete_repository":
		if !allow {
			return nil, fmt.Errorf("allow_destructive=true required")
		}
		err := dc.Resource(gvrRepo).Namespace(ns).Delete(ctx, strArg(args, "name", ""), metav1.DeleteOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": strArg(args, "name", "")}, nil
	case "list_plans":
		list, err := dc.Resource(gvrPlan).Namespace(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return list.Items, nil
	case "create_plan":
		obj, err := bodyToUnstructured(args["body"], "BackupPlan", ns)
		if err != nil {
			return nil, err
		}
		return dc.Resource(gvrPlan).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	case "delete_plan":
		if !allow {
			return nil, fmt.Errorf("allow_destructive=true required")
		}
		err := dc.Resource(gvrPlan).Namespace(ns).Delete(ctx, strArg(args, "name", ""), metav1.DeleteOptions{})
		if err != nil {
			return nil, err
		}
		return map[string]string{"deleted": strArg(args, "name", "")}, nil
	case "trigger_backup":
		obj, err := bodyToUnstructured(args["body"], "PVCBackup", ns)
		if err != nil {
			return nil, err
		}
		return dc.Resource(gvrBackup).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	case "create_pvc_restore":
		obj, err := bodyToUnstructured(args["body"], "PVCRestore", ns)
		if err != nil {
			return nil, err
		}
		return dc.Resource(gvrRestore).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	case "get_pvc_restore":
		return dc.Resource(gvrRestore).Namespace(ns).Get(ctx, strArg(args, "name", ""), metav1.GetOptions{})
	case "restore_export":
		repoNS := strArg(args, "repository_namespace", ns)
		snapshotID := strArg(args, "snapshot_id", "latest")
		ttl := intArg(args, "ttl_seconds", 3600)
		pathFilters := stringSliceArg(args, "path_filters")
		obj := &unstructured.Unstructured{Object: map[string]interface{}{
			"apiVersion": "operator.backrest.io/v1alpha1",
			"kind":       "PVCRestore",
			"metadata": map[string]interface{}{
				"generateName": "export-",
				"namespace":    ns,
			},
			"spec": map[string]interface{}{
				"mode": "export",
				"repositoryRef": map[string]interface{}{
					"name":      strArg(args, "repository_name", ""),
					"namespace": repoNS,
				},
				"restic": map[string]interface{}{
					"snapshotID":  snapshotID,
					"pathFilters": pathFilters,
				},
				"export": map[string]interface{}{
					"enabled":    true,
					"ttlSeconds": ttl,
					"oneShot":    true,
					"format":     "tar",
				},
			},
		}}
		return dc.Resource(gvrRestore).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	case "repo_status":
		obj, err := dc.Resource(gvrRepo).Namespace(ns).Get(ctx, strArg(args, "name", ""), metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		spec, _, _ := unstructured.NestedMap(obj.Object, "spec")
		status, _, _ := unstructured.NestedMap(obj.Object, "status")
		meta, _, _ := unstructured.NestedMap(obj.Object, "metadata")
		return map[string]interface{}{
			"metadata": meta,
			"status":   status,
			"spec": map[string]interface{}{
				"url":        spec["url"],
				"appendOnly": spec["appendOnly"],
			},
		}, nil
	case "list_snapshots":
		return t.listSnapshots(ctx, dc, ns, args)
	case "get_host_config":
		return t.getHostConfig(ctx, args)
	case "index_repository":
		return t.indexRepository(ctx, dc, ns, args)
	default:
		return nil, fmt.Errorf("unknown tool %s", name)
	}
}

func (t *Tools) hostClientFromArgs(ctx context.Context, dc dynamic.Interface, ns string, args map[string]interface{}, repoName string) (*backrest.Client, string, error) {
	clusterNS := strArg(args, "cluster_namespace", "")
	clusterName := strArg(args, "cluster_name", "")
	if clusterName == "" && repoName != "" {
		obj, err := dc.Resource(gvrRepo).Namespace(ns).Get(ctx, repoName, metav1.GetOptions{})
		if err != nil {
			return nil, "", err
		}
		if v, ok, _ := unstructured.NestedString(obj.Object, "spec", "backrest", "clusterRef", "namespace"); ok && v != "" {
			clusterNS = v
		}
		if v, ok, _ := unstructured.NestedString(obj.Object, "spec", "backrest", "clusterRef", "name"); ok && v != "" {
			clusterName = v
		}
	}
	if clusterNS == "" {
		clusterNS = "backrest"
	}
	if clusterName == "" {
		clusterName = "main"
	}
	return backrest.NewClient(backrest.HostURL(clusterNS, clusterName)), clusterName, nil
}

func (t *Tools) listSnapshots(ctx context.Context, dc dynamic.Interface, ns string, args map[string]interface{}) (interface{}, error) {
	name := strArg(args, "name", "")
	planID := strArg(args, "plan_id", "")
	bc, _, err := t.hostClientFromArgs(ctx, dc, ns, args, name)
	if err != nil {
		return nil, err
	}
	snaps, err := bc.ListSnapshots(ctx, name, planID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"repository": name, "planId": planID, "snapshots": snaps, "count": len(snaps)}, nil
}

func (t *Tools) getHostConfig(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	clusterNS := strArg(args, "cluster_namespace", "backrest")
	clusterName := strArg(args, "cluster_name", "main")
	bc := backrest.NewClient(backrest.HostURL(clusterNS, clusterName))
	cfg, err := bc.GetConfig(ctx)
	if err != nil {
		return nil, err
	}
	// Redact secrets before returning to MCP clients.
	for i := range cfg.Repos {
		if cfg.Repos[i].Password != "" {
			cfg.Repos[i].Password = "***"
		}
		if len(cfg.Repos[i].Env) > 0 {
			redacted := make([]string, len(cfg.Repos[i].Env))
			for j := range cfg.Repos[i].Env {
				redacted[j] = "***"
			}
			cfg.Repos[i].Env = redacted
		}
	}
	return cfg, nil
}

func (t *Tools) indexRepository(ctx context.Context, dc dynamic.Interface, ns string, args map[string]interface{}) (interface{}, error) {
	name := strArg(args, "name", "")
	bc, _, err := t.hostClientFromArgs(ctx, dc, ns, args, name)
	if err != nil {
		return nil, err
	}
	if err := bc.IndexSnapshots(ctx, name); err != nil {
		return nil, err
	}
	return map[string]string{"indexed": name}, nil
}

func bodyToUnstructured(body interface{}, kind, ns string) (*unstructured.Unstructured, error) {
	if body == nil {
		return nil, fmt.Errorf("body is required")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	obj := &unstructured.Unstructured{}
	if err := obj.UnmarshalJSON(raw); err != nil {
		return nil, err
	}
	obj.SetAPIVersion("operator.backrest.io/v1alpha1")
	obj.SetKind(kind)
	obj.SetNamespace(ns)
	return obj, nil
}

func strArg(args map[string]interface{}, key, def string) string {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	s, ok := v.(string)
	if !ok {
		return def
	}
	if s == "" {
		return def
	}
	return s
}

func boolArg(args map[string]interface{}, key string) bool {
	if args == nil {
		return false
	}
	v, ok := args[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	return ok && b
}

func intArg(args map[string]interface{}, key string, def int) int {
	if args == nil {
		return def
	}
	v, ok := args[key]
	if !ok || v == nil {
		return def
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return def
	}
}

func stringSliceArg(args map[string]interface{}, key string) []string {
	if args == nil {
		return []string{}
	}
	v, ok := args[key]
	if !ok || v == nil {
		return []string{}
	}
	arr, ok := v.([]interface{})
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(arr))
	for _, item := range arr {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// IsNotFound reports whether err is a Kubernetes not-found error.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}
