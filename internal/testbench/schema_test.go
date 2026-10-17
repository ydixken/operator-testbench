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
	"time"

	"sigs.k8s.io/yaml"
)

const scenarioHead = "apiVersion: testbench.dev/v1alpha1\nkind: Scenario\n"

var knownSteps = map[string]bool{"valkey.shardRoles": true}

// parseScenario does what the loader does for one file: strict decode, name
// from the file, then Validate.
func parseScenario(t *testing.T, name, text string) (*Scenario, error) {
	t.Helper()
	env, err := NewCELEnv()
	if err != nil {
		t.Fatal(err)
	}
	var sc Scenario
	if err := yaml.UnmarshalStrict([]byte(text), &sc); err != nil {
		return nil, err
	}
	sc.Name, sc.Path = name, "valkey/scenarios/"+name+".yaml"
	return &sc, sc.Validate("valkey", knownSteps, env)
}

const validScenario = scenarioHead + `
labels: [smoke, scale]
priority: 2
steps:
  - name: apply
    timeout: 1m
    apply: {file: manifests/cluster.yaml}
  - name: ready
    timeout: 10m
    assert:
      object: {apiVersion: valkey.io/v1alpha1, kind: ValkeyCluster, name: demo}
      match: {status: {state: Ready, readyShards: 3}}
      cel: &ready
        - self.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')
  - name: nodes
    assert:
      list: {apiVersion: valkey.io/v1alpha1, kind: ValkeyNode, selector: valkey.io/cluster=demo}
      count: 6
      cel: ["items.all(n, n.status.ready)"]
  - name: ready again, through an alias
    assert:
      object: {apiVersion: valkey.io/v1alpha1, kind: ValkeyCluster, name: demo}
      cel: *ready
  - name: roles
    go: {name: valkey.shardRoles, args: {cluster: demo, shard: "0"}}
  - name: exec on the primary
    exec:
      pod: "{{ .Vars.primary }}"
      container: server
      command: [sh, -c, "valkey-cli -h valkey-demo.{{ .Namespace }}.svc CLUSTER INFO"]
      expect: {contains: ["cluster_state:ok"]}
  - name: exec by selector
    exec: {selector: valkey.io/cluster=demo, container: server, command: [valkey-cli, ping]}
  - name: delete the pod
    delete:
      object: {apiVersion: v1, kind: Pod, name: "{{ .Vars.primary }}"}
  - name: gone
    assert:
      object: {apiVersion: v1, kind: Service, name: valkey-demo}
      absent: true
  - name: none left
    assert:
      list: {apiVersion: v1, kind: Pod, selector: valkey.io/cluster=demo}
      count: 0
`

func TestScenarioParsesTheWholeVocabulary(t *testing.T) {
	sc, err := parseScenario(t, "cluster-ready", validScenario)
	if err != nil {
		t.Fatalf("valid scenario rejected: %v", err)
	}
	if got := StepTimeout(&sc.Steps[1]); got != 10*time.Minute {
		t.Errorf("step 2 timeout = %s, want 10m", got)
	}
	if got := StepTimeout(&sc.Steps[2]); got != DefaultStepTimeout {
		t.Errorf("step 3 timeout = %s, want the default %s", got, DefaultStepTimeout)
	}
	// Numbers decode as int64, which is what the API server hands back, so
	// DeepDerivative compares like with like.
	status, _ := sc.Steps[1].Assert.Subset()["status"].(map[string]any)
	if v, ok := status["readyShards"].(int64); !ok || v != 3 {
		t.Errorf("match readyShards decoded as %T %v, want int64 3", status["readyShards"], status["readyShards"])
	}
	if n := len(sc.Steps[3].Assert.Programs()); n != 1 {
		t.Errorf("aliased cel block compiled to %d programs, want 1", n)
	}
	for i, want := range []string{"apply", "assert", "assert", "assert", "go", "exec", "exec", "delete", "assert", "assert"} {
		if kind, err := sc.Steps[i].Kind(); err != nil || kind != want {
			t.Errorf("step %d kind = %q, %v; want %q", i+1, kind, err, want)
		}
	}
}

func TestScenarioValidationRejects(t *testing.T) {
	for _, tc := range []struct{ name, text, want string }{
		{"wrong apiVersion", "apiVersion: testbench.dev/v1\nkind: Scenario\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "apiVersion"},
		{"wrong kind", "apiVersion: testbench.dev/v1alpha1\nkind: OperatorPack\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "kind \"OperatorPack\""},
		{"unknown field", scenarioHead + "stepz: []\n", "unknown field"},
		{"no steps", scenarioHead + "steps: []\n", "no steps"},
		{"nameless step", scenarioHead + "steps: [{apply: {file: m.yaml}}]\n", "no name"},
		{"two kinds", scenarioHead + "steps: [{name: a, apply: {file: m.yaml}, go: {name: valkey.shardRoles}}]\n", "2 kinds"},
		{"no kind", scenarioHead + "steps: [{name: a, timeout: 1m}]\n", "no kind"},
		{"zero timeout", scenarioHead + "steps: [{name: a, timeout: 0s, apply: {file: m.yaml}}]\n", "not positive"},
		{"bad label chars", scenarioHead + "labels: ['a&b']\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "contains one of"},
		{"label starting with colon", scenarioHead + "labels: [':x']\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "starts with ':'"},
		{"label with empty suffix", scenarioHead + "labels: ['set:']\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "nothing after"},
		{"duplicate label", scenarioHead + "labels: [smoke, smoke]\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "listed twice"},
		{"negative priority", scenarioHead + "priority: -1\nsteps: [{name: a, apply: {file: m.yaml}}]\n", "negative"},
		{"apply escaping the pack", scenarioHead + "steps: [{name: a, apply: {file: ../other.yaml}}]\n", "inside the pack"},
		{"assert without target", scenarioHead + "steps: [{name: a, assert: {cel: ['true']}}]\n", "object or list"},
		{"assert with both", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, list: {apiVersion: v1, kind: Pod, selector: a=b}}}]\n", "both object and list"},
		{"absent with match", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, absent: true, match: {x: y}}}]\n", "absent excludes"},
		{"count on object", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, count: 1}}]\n", "count applies to a list"},
		{"match on list", scenarioHead + "steps: [{name: a, assert: {list: {apiVersion: v1, kind: Pod, selector: a=b}, match: {x: y}}}]\n", "match applies to an object"},
		{"absent on list", scenarioHead + "steps: [{name: a, assert: {list: {apiVersion: v1, kind: Pod, selector: a=b}, absent: true}}]\n", "count: 0"},
		{"bare list", scenarioHead + "steps: [{name: a, assert: {list: {apiVersion: v1, kind: Pod, selector: a=b}}}]\n", "needs count or cel"},
		{"negative count", scenarioHead + "steps: [{name: a, assert: {list: {apiVersion: v1, kind: Pod, selector: a=b}, count: -1}}]\n", "negative"},
		{"bad selector", scenarioHead + "steps: [{name: a, assert: {list: {apiVersion: v1, kind: Pod, selector: 'a==='}, count: 1}}]\n", "selector"},
		{"bad apiVersion in ref", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: a/b/c, kind: Pod, name: p}}}]\n", "apiVersion"},
		{"empty string in match", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, match: {status: {phase: ''}}}}]\n", "empty string"},
		{"empty list in match", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, match: {status: {conditions: []}}}}]\n", "empty list"},
		{"empty map in match", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, match: {status: {}}}}]\n", "empty map"},
		{"null in match", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, match: {status: null}}}]\n", "null"},
		{"non-bool cel", scenarioHead + "steps: [{name: a, assert: {object: {apiVersion: v1, kind: Pod, name: p}, cel: ['self.status.phase']}}]\n", "must evaluate to bool"},
		{"exec without target", scenarioHead + "steps: [{name: a, exec: {container: c, command: [ls]}}]\n", "pod or selector"},
		{"exec with both", scenarioHead + "steps: [{name: a, exec: {pod: p, selector: a=b, container: c, command: [ls]}}]\n", "both pod and selector"},
		{"exec without container", scenarioHead + "steps: [{name: a, exec: {pod: p, command: [ls]}}]\n", "needs container"},
		{"exec without command", scenarioHead + "steps: [{name: a, exec: {pod: p, container: c}}]\n", "needs command"},
		{"exec empty contains", scenarioHead + "steps: [{name: a, exec: {pod: p, container: c, command: [ls], expect: {contains: ['']}}}]\n", "contains[0] is empty"},
		{"exec bad template", scenarioHead + "steps: [{name: a, exec: {pod: '{{ .Vars.primary', container: c, command: [ls]}}]\n", "template"},
		{"delete without target", scenarioHead + "steps: [{name: a, delete: {}}]\n", "object or list"},
		{"unknown go step", scenarioHead + "steps: [{name: a, go: {name: valkey.nope}}]\n", "not registered"},
		{"go arg bad template", scenarioHead + "steps: [{name: a, go: {name: valkey.shardRoles, args: {x: '{{ bad'}}}]\n", "template"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseScenario(t, "s", tc.text)
			if err == nil {
				t.Fatalf("accepted; want an error containing %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err, tc.want)
			}
		})
	}
}

func TestScenarioNameRules(t *testing.T) {
	minimal := scenarioHead + "steps: [{name: a, apply: {file: m.yaml}}]\n"
	for _, tc := range []struct{ name, want string }{
		{"Cluster-Ready", "DNS label"},
		{"cluster_ready", "DNS label"},
		{"-leading", "DNS label"},
		{strings.Repeat("a", 31), "longer than 30"},
	} {
		if _, err := parseScenario(t, tc.name, minimal); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("name %q: got %v, want an error containing %q", tc.name, err, tc.want)
		}
	}
	if _, err := parseScenario(t, strings.Repeat("a", 30), minimal); err != nil {
		t.Errorf("a 30 character name must fit beside a 16 character pack and -999: %v", err)
	}
	if got := NamespaceFor("valkey", "cluster-ready", 1); got != "testbench-valkey-cluster-ready" {
		t.Errorf("NamespaceFor proc 1 = %q", got)
	}
	if got := NamespaceFor("valkey", "cluster-ready", 4); got != "testbench-valkey-cluster-ready-4" {
		t.Errorf("NamespaceFor proc 4 = %q", got)
	}
}

func TestValidationReportsEveryBrokenStep(t *testing.T) {
	text := scenarioHead + "steps:\n  - {name: one, exec: {container: c, command: [ls]}}\n  - {name: two, delete: {}}\n"
	_, err := parseScenario(t, "s", text)
	if err == nil {
		t.Fatal("accepted two broken steps")
	}
	for _, want := range []string{`step 1 "one"`, `step 2 "two"`, "valkey/scenarios/s.yaml"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q lacks %q", err, want)
		}
	}
}

const validPack = `apiVersion: testbench.dev/v1alpha1
kind: OperatorPack
operator:
  version: v0.7.1
  namespace: testbench-valkey-system
  selector: app.kubernetes.io/name=valkey-operator,control-plane=controller-manager
install:
  crds:
    - https://raw.githubusercontent.com/valkey-io/valkey-operator/{{ .Version }}/config/crd/bases/valkey.io_valkeyclusters.yaml
  helm:
    repo: https://valkey.io/valkey-helm
    chart: valkey-operator
    version: 0.7.0
    release: testbench-valkey-operator
    set: {image.tag: "{{ .Version }}"}
`

func parsePack(t *testing.T, text string) error {
	t.Helper()
	var p OperatorPack
	if err := yaml.UnmarshalStrict([]byte(text), &p); err != nil {
		return err
	}
	return p.Validate()
}

func TestPackValidation(t *testing.T) {
	if err := parsePack(t, validPack); err != nil {
		t.Fatalf("valid pack rejected: %v", err)
	}
	for _, tc := range []struct{ name, from, to, want string }{
		{"wrong kind", "kind: OperatorPack", "kind: Scenario", "kind \"Scenario\""},
		{"version with slash", "version: v0.7.1", "version: refs/tags/v0.7.1", "single token"},
		{"bad namespace", "namespace: testbench-valkey-system", "namespace: Valkey_System", "operator.namespace"},
		{"bad selector", "selector: app.kubernetes.io/name=valkey-operator,control-plane=controller-manager", "selector: 'a==='", "operator.selector"},
		{"http crd", "- https://raw.githubusercontent.com", "- http://raw.githubusercontent.com", "https URL"},
		{"crd bad template", "{{ .Version }}/config", "{{ .Version /config", "install.crds[0]"},
		{"helm repo not https", "repo: https://valkey.io/valkey-helm", "repo: oci://ghcr.io/x", "install.helm.repo"},
		{"empty chart", "chart: valkey-operator", "chart: ''", "install.helm.chart"},
		{"empty chart version", "version: 0.7.0", "version: ''", "install.helm.version"},
		{"bad release", "release: testbench-valkey-operator", "release: Testbench", "install.helm.release"},
		{"set bad template", `set: {image.tag: "{{ .Version }}"}`, `set: {image.tag: "{{ .Version"}`, "install.helm.set[image.tag]"},
		{"unknown field", "install:", "installs:", "unknown field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			text := strings.Replace(validPack, tc.from, tc.to, 1)
			if text == validPack {
				t.Fatalf("replacement %q not found in the fixture", tc.from)
			}
			err := parsePack(t, text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
	if err := parsePack(t, strings.Replace(validPack, "  helm:", "  helmx:", 1)); err == nil {
		t.Error("a pack without install.helm was accepted")
	}
}
