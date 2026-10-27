/*
Copyright 2026 operator-testbench contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package e2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/ydixken/operator-testbench/internal/testbench"
	"github.com/ydixken/operator-testbench/operators"
)

// TestPackDir validates the packs in TESTBENCH_PACK_DIR without a cluster:
// the same loader a run uses, with the Go steps this binary registers. task
// pack:validate runs it, and so does the image's e2e.test. Without the
// variable there is nothing to validate.
func TestPackDir(t *testing.T) {
	dir := os.Getenv(testbench.EnvPackDir)
	if dir == "" {
		t.Skip(testbench.EnvPackDir + " is not set; task pack:validate PACK_DIR=<dir> sets it")
	}
	fsys := os.DirFS(dir)
	packs, err := testbench.ListPacks(fsys)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatalf("%s holds no pack: want <dir>/<pack>/operator.yaml, like operators/", dir)
	}
	for _, name := range packs {
		pack, err := testbench.LoadPack(fsys, name, "", goStepNames())
		if err != nil {
			t.Errorf("pack %s: %v", name, err)
			continue
		}
		t.Logf("pack %s: operator %s, %d scenarios", name, pack.Version, len(pack.Scenarios))
	}
}

// copyPackFile copies one embedded pack file to dir/to, so a test can build
// an external pack from the shipped one.
func copyPackFile(t *testing.T, dir, from, to string) {
	t.Helper()
	data, err := fs.ReadFile(operators.FS, from)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, to)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// With TESTBENCH_PACK_DIR the suite reads the pack from that directory and
// nothing from the embedded one: external-smoke exists only outside.
func TestLoadSuiteReadsThePackDir(t *testing.T) {
	dir := t.TempDir()
	copyPackFile(t, dir, "valkey/operator.yaml", "valkey/operator.yaml")
	copyPackFile(t, dir, "valkey/manifests/cluster-3x1.yaml", "valkey/manifests/cluster-3x1.yaml")
	copyPackFile(t, dir, "valkey/scenarios/cluster-ready.yaml", "valkey/scenarios/external-smoke.yaml")
	env := map[string]string{
		testbench.EnvContext: "kind-testbench", testbench.EnvOperator: "valkey", testbench.EnvPackDir: dir,
	}
	s, err := loadSuite(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Pack.Scenarios) != 1 || s.Pack.Scenarios[0].Name != "external-smoke" {
		t.Errorf("scenarios = %v, want only external-smoke from %s", s.Pack.Scenarios, dir)
	}

	t.Setenv(testbench.EnvPackDir, dir)
	TestPackDir(t)
}
