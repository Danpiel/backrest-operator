package mcp_test

import (
	"context"
	"testing"

	"github.com/Danpiel/backrest-operator/internal/mcp"
)

func TestDestructiveToolsSet(t *testing.T) {
	want := []string{"delete_repository", "delete_plan", "delete_snapshot"}
	if len(mcp.DestructiveTools) != len(want) {
		t.Fatalf("size mismatch: %v", mcp.DestructiveTools)
	}
	for _, k := range want {
		if _, ok := mcp.DestructiveTools[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestAuthorizeDestructiveWithoutFlag(t *testing.T) {
	denies := 0
	a := mcp.NewAuthForTest(func(tool string) { denies++ })
	user := &mcp.UserIdentity{Username: "alice", Groups: []string{"devs"}}
	for tool := range mcp.DestructiveTools {
		if a.AuthorizeTool(context.Background(), user, tool, "app", false) {
			t.Fatalf("expected deny for %s", tool)
		}
	}
	if denies != len(mcp.DestructiveTools) {
		t.Fatalf("expected %d denials, got %d", len(mcp.DestructiveTools), denies)
	}
}

func TestAuthorizeUnknownTool(t *testing.T) {
	denies := 0
	a := mcp.NewAuthForTest(func(tool string) { denies++ })
	user := &mcp.UserIdentity{Username: "alice"}
	if a.AuthorizeTool(context.Background(), user, "nonexistent_tool", "app", true) {
		t.Fatal("expected deny")
	}
	if denies != 1 {
		t.Fatalf("denies=%d", denies)
	}
}

func TestStdioBypassesSAR(t *testing.T) {
	a := mcp.NewAuthForTest(nil)
	user := &mcp.UserIdentity{Username: mcp.StdioUsername, Groups: []string{"system:authenticated"}}
	if !a.AuthorizeTool(context.Background(), user, "list_plans", "app", false) {
		t.Fatal("stdio should allow non-destructive")
	}
	if a.AuthorizeTool(context.Background(), user, "delete_plan", "app", false) {
		t.Fatal("stdio still requires allow_destructive")
	}
	if !a.AuthorizeTool(context.Background(), user, "delete_plan", "app", true) {
		t.Fatal("stdio should allow with flag")
	}
}

func TestToolSchemasNonEmpty(t *testing.T) {
	if len(mcp.ToolSchemas()) < 10 {
		t.Fatal("expected tool schemas")
	}
}
