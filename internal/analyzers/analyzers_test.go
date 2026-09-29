package analyzers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/leelapavan/sfdc-agentops/internal/config"
	"github.com/leelapavan/sfdc-agentops/internal/metadata"
)

func TestOrgInventorySuppressesMissingDependency(t *testing.T) {
	root := t.TempDir()
	ps := "force-app/main/default/permissionsets/P.permissionset-meta.xml"
	os.MkdirAll(filepath.Join(root, filepath.Dir(ps)), 0o755)
	os.WriteFile(filepath.Join(root, ps), []byte(`<PermissionSet><fieldPermissions><field>Account.Tier__c</field></fieldPermissions></PermissionSet>`), 0o644)

	changes := []metadata.Change{{Status: metadata.Modified, Path: ps}}
	run := func(cfg config.Config) []Finding {
		return Run(&Context{Root: root, Changes: changes, Components: metadata.Components(changes), Config: cfg})
	}
	if fs := run(config.Default()); len(fs) != 1 || fs[0].RuleID != "DEP001" {
		t.Fatalf("want DEP001 without inventory, got %+v", fs)
	}
	os.WriteFile(filepath.Join(root, "inventory.txt"), []byte("# org-only fields\nCustomField:Account.Tier__c\n"), 0o644)
	cfg := config.Default()
	cfg.OrgInventory = "inventory.txt"
	if fs := run(cfg); len(fs) != 0 {
		t.Fatalf("want no findings with inventory, got %+v", fs)
	}
}
