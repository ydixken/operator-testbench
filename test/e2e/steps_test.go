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
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/ydixken/operator-testbench/internal/testbench"
	"github.com/ydixken/operator-testbench/operators"
)

// goSteps is the registry of Go escape hatches a scenario may name as
// `go: {name: ...}`. One explicit map, no init() registration: the loader
// validates against exactly these keys, and TestEmbeddedPacksParse requires
// every key to be used by some scenario, so nothing here is dead.
var goSteps = map[string]testbench.GoStepFunc{
	"valkey.shardRoles": valkeyShardRoles,
}

func goStepNames() map[string]bool {
	known := make(map[string]bool, len(goSteps))
	for name := range goSteps {
		known[name] = true
	}
	return known
}

// Every embedded pack parses without a cluster, has at least one scenario, and
// uses every registered Go step. This is what makes a typo in a scenario fail
// `task test` instead of a kind run.
func TestEmbeddedPacksParse(t *testing.T) {
	packs, err := testbench.ListPacks(operators.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(packs) == 0 {
		t.Fatal("no packs under operators/")
	}
	used := map[string]bool{}
	for _, name := range packs {
		pack, err := testbench.LoadPack(operators.FS, name, "", goStepNames())
		if err != nil {
			t.Errorf("pack %s: %v", name, err)
			continue
		}
		if len(pack.Scenarios) == 0 {
			t.Errorf("pack %s has no scenarios", name)
		}
		for _, sc := range pack.Scenarios {
			for _, st := range sc.Steps {
				if st.Go != nil {
					used[st.Go.Name] = true
				}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(goSteps)) {
		if !used[name] {
			t.Errorf("go step %s is registered but no scenario uses it", name)
		}
	}
}

func TestLoadSuiteSkipsWithoutContext(t *testing.T) {
	getenv := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if s, err := loadSuite(getenv(nil)); s != nil || err != nil {
		t.Errorf("loadSuite without %s = %v, %v; want nil, nil", testbench.EnvContext, s, err)
	}
	if describeScenarios(nil) {
		t.Error("describeScenarios(nil) built a tree")
	}
	_, err := loadSuite(getenv(map[string]string{testbench.EnvContext: "kind-testbench"}))
	if err == nil || !strings.Contains(err.Error(), testbench.EnvOperator) {
		t.Errorf("loadSuite without a pack = %v; want an error naming %s", err, testbench.EnvOperator)
	}
	_, err = loadSuite(getenv(map[string]string{testbench.EnvContext: "kind-testbench", testbench.EnvOperator: "nope"}))
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("loadSuite with an unknown pack = %v; want an error naming it", err)
	}
	s, err := loadSuite(getenv(map[string]string{testbench.EnvContext: "kind-testbench", testbench.EnvOperator: "valkey"}))
	if err != nil || s == nil || s.Pack.Name != "valkey" || s.Env.Install != testbench.InstallHelm {
		t.Errorf("loadSuite for valkey = %+v, %v", s, err)
	}
}
