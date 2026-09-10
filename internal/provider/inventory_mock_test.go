package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestTerraformInventoryMocksKeepDistinctEntries(t *testing.T) {
	terraform, err := exec.LookPath("terraform")
	if err != nil {
		if os.Getenv("TF_ACC") == "1" {
			t.Fatal(err)
		}
		t.Skip("terraform is not installed")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	dir := t.TempDir()
	build := exec.CommandContext(ctx, "go", "build", "-o", filepath.Join(dir, "terraform-provider-pmon"), "../..")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build provider: %v\n%s", err, output)
	}
	config := filepath.Join(dir, "terraform.rc")
	contents := fmt.Sprintf("provider_installation {\n  dev_overrides {\n    \"ridi-oss/pmon\" = %q\n  }\n  direct {}\n}\n", dir)
	if err := os.WriteFile(config, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TF_CLI_CONFIG_FILE", config)
	t.Setenv("TF_DATA_DIR", filepath.Join(dir, "data"))
	command := exec.CommandContext(ctx, terraform, "-chdir=testdata/inventory-mocks", "test", "-no-color")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("terraform test: %v\n%s", err, output)
	}
}
