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

package testbench

import (
	"strings"
	"testing"
	"testing/fstest"
)

const (
	minimalScenario = scenarioHead + "steps: [{name: apply, apply: {file: manifests/cluster.yaml}}]\n"
	manifest        = "apiVersion: v1\nkind: ConfigMap\nmetadata: {name: demo}\n"
)

// packFS is the valid pack every loader test starts from.
func packFS() fstest.MapFS {
	return fstest.MapFS{
		"valkey/operator.yaml":                {Data: []byte(validPack)},
		"valkey/manifests/cluster.yaml":       {Data: []byte(manifest)},
		"valkey/scenarios/cluster-ready.yaml": {Data: []byte(minimalScenario)},
		"valkey/scenarios/alpha.yaml":         {Data: []byte(minimalScenario)},
		"other/operator.yaml":                 {Data: []byte(validPack)},
		"other/scenarios/one.yaml":            {Data: []byte(minimalScenario)},
		"other/manifests/cluster.yaml":        {Data: []byte(manifest)},
		"notapack/README.md":                  {Data: []byte("no operator.yaml here")},
	}
}

func TestListPacks(t *testing.T) {
	got, err := ListPacks(packFS())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "other,valkey" {
		t.Errorf("ListPacks = %v, want [other valkey]: sorted, only directories with an operator.yaml", got)
	}
}

func TestLoadPackRendersAndSortsScenarios(t *testing.T) {
	p, err := LoadPack(packFS(), "valkey", "", knownSteps)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v0.7.1" {
		t.Errorf("Version = %q, want the pack's own v0.7.1", p.Version)
	}
	wantCRD := "https://raw.githubusercontent.com/valkey-io/valkey-operator/v0.7.1/config/crd/bases/valkey.io_valkeyclusters.yaml"
	if len(p.CRDs) != 1 || p.CRDs[0] != wantCRD {
		t.Errorf("CRDs = %v, want [%s]", p.CRDs, wantCRD)
	}
	if p.HelmSet["image.tag"] != "v0.7.1" {
		t.Errorf("HelmSet[image.tag] = %q, want v0.7.1", p.HelmSet["image.tag"])
	}
	names := make([]string, 0, len(p.Scenarios))
	for _, sc := range p.Scenarios {
		names = append(names, sc.Name)
	}
	if strings.Join(names, ",") != "alpha,cluster-ready" {
		t.Errorf("scenarios = %v, want file order [alpha cluster-ready]", names)
	}
	if p.Scenarios[0].Path != "valkey/scenarios/alpha.yaml" {
		t.Errorf("Path = %q", p.Scenarios[0].Path)
	}
}

func TestPackNamespaces(t *testing.T) {
	p, err := LoadPack(packFS(), "valkey", "", knownSteps)
	if err != nil {
		t.Fatal(err)
	}
	for procs, want := range map[int]string{
		0: "testbench-valkey-alpha,testbench-valkey-cluster-ready",
		2: "testbench-valkey-alpha,testbench-valkey-cluster-ready,testbench-valkey-alpha-2,testbench-valkey-cluster-ready-2",
	} {
		if got := strings.Join(p.Namespaces(procs), ","); got != want {
			t.Errorf("Namespaces(%d) = %s, want %s", procs, got, want)
		}
	}
}

func TestLoadPackVersionOverrideWins(t *testing.T) {
	p, err := LoadPack(packFS(), "valkey", "v0.8.0-rc.1", knownSteps)
	if err != nil {
		t.Fatal(err)
	}
	if p.Version != "v0.8.0-rc.1" || !strings.Contains(p.CRDs[0], "/v0.8.0-rc.1/") || p.HelmSet["image.tag"] != "v0.8.0-rc.1" {
		t.Errorf("override not applied: version %q, crds %v, set %v", p.Version, p.CRDs, p.HelmSet)
	}
}

func TestLoadPackRejects(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(fsys fstest.MapFS)
		pack string
		want string
	}{
		{"unknown pack", func(fstest.MapFS) {}, "nope", "operator.yaml"},
		{"pack name too long", func(fstest.MapFS) {}, strings.Repeat("a", 17), "longer than 16"},
		{"stray file in scenarios", func(f fstest.MapFS) { f["valkey/scenarios/notes.txt"] = &fstest.MapFile{Data: []byte("x")} }, "valkey", "only *.yaml"},
		{"missing manifest", func(f fstest.MapFS) {
			f["valkey/scenarios/alpha.yaml"] = &fstest.MapFile{Data: []byte(scenarioHead + "steps: [{name: a, apply: {file: manifests/missing.yaml}}]\n")}
		}, "valkey", `apply.file "manifests/missing.yaml"`},
		{"namespace in operator.yaml", func(f fstest.MapFS) {
			f["valkey/operator.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(validPack, "{{ .Version }}/config", "{{ .Namespace }}/config", 1))}
		}, "valkey", "install.crds[0]"},
		{"no scenarios", func(f fstest.MapFS) {
			delete(f, "valkey/scenarios/alpha.yaml")
			delete(f, "valkey/scenarios/cluster-ready.yaml")
		}, "valkey", "no scenarios"},
		{"invalid pack spec", func(f fstest.MapFS) {
			f["valkey/operator.yaml"] = &fstest.MapFile{Data: []byte(strings.Replace(validPack, "chart: valkey-operator", "chart: ''", 1))}
		}, "valkey", "valkey/operator.yaml: install.helm.chart"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fsys := packFS()
			tc.edit(fsys)
			_, err := LoadPack(fsys, tc.pack, "", knownSteps)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestLoadPackReportsEveryBrokenFile(t *testing.T) {
	fsys := packFS()
	broken := scenarioHead + "steps: [{name: a, exec: {container: c, command: [ls]}}]\n"
	fsys["valkey/scenarios/alpha.yaml"] = &fstest.MapFile{Data: []byte(broken)}
	fsys["valkey/scenarios/cluster-ready.yaml"] = &fstest.MapFile{Data: []byte(broken)}
	_, err := LoadPack(fsys, "valkey", "", knownSteps)
	if err == nil {
		t.Fatal("two broken scenarios were accepted")
	}
	for _, want := range []string{"valkey/scenarios/alpha.yaml", "valkey/scenarios/cluster-ready.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %s", err, want)
		}
	}
}

func TestRender(t *testing.T) {
	data := TemplateData{Namespace: "testbench-valkey-demo", Version: "v0.7.1", Vars: map[string]string{"primary": "valkey-demo-0-0-0"}}
	got, err := Render("valkey-demo.{{ .Namespace }}.svc {{ .Vars.primary }} {{ .Version }}", data)
	if err != nil || got != "valkey-demo.testbench-valkey-demo.svc valkey-demo-0-0-0 v0.7.1" {
		t.Errorf("Render = %q, %v", got, err)
	}
	if _, err := Render("{{ .Vars.replica }}", data); err == nil {
		t.Error("a missing var rendered as empty instead of failing")
	}
	if _, err := Render("{{ .Nope }}", data); err == nil {
		t.Error("a missing field rendered instead of failing")
	}
}
