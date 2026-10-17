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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var (
	valkeyGV        = schema.GroupVersion{Group: "valkey.io", Version: "v1alpha1"}
	valkeyCluster   = valkeyGV.WithKind("ValkeyCluster")
	testNamespace   = NamespaceFor("valkey", "demo", 1)
	clusterManifest = `apiVersion: valkey.io/v1alpha1
kind: ValkeyCluster
metadata:
  name: demo
spec:
  shards: 3
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: notes
data:
  ns: "{{ .Namespace }}"
---
`
)

// mapper knows which kinds are namespaced; the fake client asks it for
// IsObjectNamespaced.
func mapper() meta.RESTMapper {
	m := meta.NewDefaultRESTMapper([]schema.GroupVersion{corev1.SchemeGroupVersion, valkeyGV})
	for _, kind := range []string{"Pod", "ConfigMap", "Secret", "Service"} {
		m.Add(corev1.SchemeGroupVersion.WithKind(kind), meta.RESTScopeNamespace)
	}
	m.Add(corev1.SchemeGroupVersion.WithKind("Namespace"), meta.RESTScopeRoot)
	m.Add(valkeyCluster, meta.RESTScopeNamespace)
	m.Add(valkeyGV.WithKind("ValkeyNode"), meta.RESTScopeNamespace)
	return m
}

// fakeCommands records every argv and answers each call with the script at
// that index (the last one repeats), run through sh.
type fakeCommands struct {
	mu      sync.Mutex
	argv    [][]string
	scripts []string
}

func (f *fakeCommands) factory(ctx context.Context, name string, args ...string) *exec.Cmd {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.argv = append(f.argv, append([]string{name}, args...))
	script := f.scripts[min(len(f.argv)-1, len(f.scripts)-1)]
	return exec.CommandContext(ctx, "sh", "-c", script)
}

func (f *fakeCommands) calls() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.argv
}

type harness struct {
	run  *Run
	log  *bytes.Buffer
	cmds *fakeCommands
}

func newHarness(t *testing.T, steps map[string]GoStepFunc, objs ...client.Object) *harness {
	t.Helper()
	c := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithRESTMapper(mapper()).WithObjects(objs...).Build()
	pack := &Pack{Name: "valkey", Version: "v0.7.1", FS: fstest.MapFS{
		"valkey/manifests/cluster.yaml":  {Data: []byte(clusterManifest)},
		"valkey/manifests/other-ns.yaml": {Data: []byte("apiVersion: v1\nkind: ConfigMap\nmetadata: {name: x, namespace: elsewhere}\n")},
		"valkey/manifests/nameless.yaml": {Data: []byte("apiVersion: v1\nkind: ConfigMap\n")},
	}}
	h := &harness{log: &bytes.Buffer{}, cmds: &fakeCommands{scripts: []string{"echo cluster_state:ok"}}}
	h.run = NewRun(c, "kind-testbench", pack, &Scenario{Name: "demo"}, 1, Env{}, steps, h.cmds.factory, h.log)
	return h
}

func pod(name string, phase corev1.PodPhase, labels map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace, Labels: labels, UID: types.UID("uid-" + name)},
		Status:     corev1.PodStatus{Phase: phase},
	}
}

func step(t *testing.T, text string) *Step {
	t.Helper()
	sc, err := parseScenario(t, "demo", scenarioHead+"steps:\n"+text)
	if err != nil {
		t.Fatalf("step fixture: %v", err)
	}
	return &sc.Steps[0]
}

func TestEnsureNamespaceCreatesThenAdopts(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if err := h.run.EnsureNamespace(ctx); err != nil {
		t.Fatal(err)
	}
	var ns corev1.Namespace
	if err := h.run.Client.Get(ctx, client.ObjectKey{Name: testNamespace}, &ns); err != nil {
		t.Fatal(err)
	}
	if ns.Labels[LabelPack] != "valkey" || ns.Labels[LabelScenario] != "demo" || ns.Labels[LabelManagedBy] != FieldOwner {
		t.Errorf("namespace labels = %v", ns.Labels)
	}
	if err := h.run.EnsureNamespace(ctx); err != nil || !strings.Contains(h.log.String(), "reusing it") {
		t.Errorf("second EnsureNamespace = %v, log %q; want adoption", err, h.log.String())
	}
}

func TestApplyDefaultsTheNamespaceAndRecordsOnce(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	st := step(t, "  - {name: apply, apply: {file: manifests/cluster.yaml}}\n")
	for range 2 {
		if err := h.run.Step(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(valkeyCluster)
	if err := h.run.Client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "demo"}, u); err != nil {
		t.Fatalf("ValkeyCluster not applied into the scenario namespace: %v", err)
	}
	var cm corev1.ConfigMap
	if err := h.run.Client.Get(ctx, client.ObjectKey{Namespace: testNamespace, Name: "notes"}, &cm); err != nil {
		t.Fatal(err)
	}
	if cm.Data["ns"] != testNamespace {
		t.Errorf("manifest template rendered %q, want the scenario namespace", cm.Data["ns"])
	}
	if len(h.run.applied) != 2 {
		t.Errorf("recorded %d applied objects after two applies of two objects, want 2", len(h.run.applied))
	}
	for _, tc := range []struct{ file, want string }{
		{"manifests/other-ns.yaml", "only live in the scenario namespace"},
		{"manifests/nameless.yaml", "needs apiVersion, kind and metadata.name"},
	} {
		err := h.run.Step(ctx, step(t, fmt.Sprintf("  - {name: apply, apply: {file: %s}}\n", tc.file)))
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: got %v, want %q", tc.file, err, tc.want)
		}
	}
}

func TestAssertObject(t *testing.T) {
	ready := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "valkey.io/v1alpha1", "kind": "ValkeyCluster",
		"metadata": map[string]any{"name": "demo", "namespace": testNamespace},
		"status":   map[string]any{"state": "Ready", "readyShards": int64(3), "podIP": "10.0.0.9"},
	}}
	h := newHarness(t, nil, ready)
	ctx := context.Background()
	const ref = "object: {apiVersion: valkey.io/v1alpha1, kind: ValkeyCluster, name: demo}"
	for _, tc := range []struct{ name, assert, want string }{
		{"match and cel pass", "{" + ref + ", match: {status: {state: Ready}}, cel: ['self.status.readyShards == 3']}", ""},
		{"match fails", "{" + ref + ", match: {status: {state: Failed}}}", "expected subset"},
		{"cel fails with the object attached", "{" + ref + ", cel: ['self.status.readyShards == 4']}", "object:"},
		{"absent while present", "{" + ref + ", absent: true}", "still exists"},
		{"absent once gone", "{object: {apiVersion: v1, kind: Service, name: nope}, absent: true}", ""},
		{"missing object", "{object: {apiVersion: v1, kind: Service, name: nope}}", "not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			st := step(t, "  - {name: a, timeout: 1s, assert: "+tc.assert+"}\n")
			err := h.run.Step(ctx, st)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("got %v, want success", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("got %v, want an error containing %q", err, tc.want)
			case err != nil && strings.Contains(err.Error(), "10.0.0.9"):
				t.Errorf("failure message leaks the pod IP: %v", err)
			}
		})
	}
}

func TestAssertList(t *testing.T) {
	sel := map[string]string{"valkey.io/cluster": "demo"}
	h := newHarness(t, nil, pod("a", corev1.PodRunning, sel), pod("b", corev1.PodPending, sel), pod("other", corev1.PodRunning, nil))
	ctx := context.Background()
	const list = "list: {apiVersion: v1, kind: Pod, selector: valkey.io/cluster=demo}"
	for _, tc := range []struct{ name, assert, want string }{
		{"count and cel pass", "{" + list + ", count: 2, cel: ['items.exists(p, p.status.phase == \"Running\")']}", ""},
		{"count fails", "{" + list + ", count: 3}", "2 Pod match valkey.io/cluster=demo, want 3"},
		{"cel fails", "{" + list + ", cel: ['items.all(p, p.status.phase == \"Running\")']}", "items: 2 Pod"},
		{"count zero on an empty selection", "{list: {apiVersion: v1, kind: Pod, selector: none=here}, count: 0}", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := h.run.Step(ctx, step(t, "  - {name: a, timeout: 1s, assert: "+tc.assert+"}\n"))
			if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
				t.Errorf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestExecPicksAPodAndChecksOutput(t *testing.T) {
	sel := map[string]string{"valkey.io/cluster": "demo"}
	h := newHarness(t, nil, pod("c", corev1.PodRunning, sel), pod("a", corev1.PodPending, sel), pod("b", corev1.PodRunning, sel))
	ctx := context.Background()
	st := step(t, "  - name: e\n    timeout: 5s\n    exec:\n      selector: valkey.io/cluster=demo\n      container: server\n      command: [sh, -c, 'valkey-cli -h valkey-demo.{{ .Namespace }}.svc CLUSTER INFO']\n      expect: {contains: ['cluster_state:ok']}\n")
	if err := h.run.Step(ctx, st); err != nil {
		t.Fatal(err)
	}
	calls := h.cmds.calls()
	want := []string{"kubectl", "--context", "kind-testbench", "exec", "-n", testNamespace, "b", "-c", "server", "--",
		"sh", "-c", "valkey-cli -h valkey-demo." + testNamespace + ".svc CLUSTER INFO"}
	if len(calls) != 1 || strings.Join(calls[0], " ") != strings.Join(want, " ") {
		t.Errorf("argv = %q, want %q", calls, want)
	}
}

func TestExecRetriesAndRedacts(t *testing.T) {
	h := newHarness(t, nil, pod("p", corev1.PodRunning, nil))
	h.cmds.scripts = []string{"echo boom >&2; exit 1", "echo master_host:10.244.1.5; echo role:master"}
	ctx := context.Background()
	st := step(t, "  - {name: e, timeout: 10s, exec: {pod: p, container: server, command: [valkey-cli, INFO, replication], expect: {contains: ['role:master']}}}\n")
	if err := h.run.Step(ctx, st); err != nil {
		t.Fatalf("a failed first attempt must be retried: %v", err)
	}
	if n := len(h.cmds.calls()); n != 2 {
		t.Errorf("%d attempts, want 2", n)
	}
	st = step(t, "  - {name: e, timeout: 1s, exec: {pod: p, container: server, command: [valkey-cli, INFO, replication], expect: {contains: ['role:slave']}}}\n")
	err := h.run.Step(ctx, st)
	if err == nil || !strings.Contains(err.Error(), "lacks") || !strings.Contains(err.Error(), "[redacted ip]") || strings.Contains(err.Error(), "10.244.1.5") {
		t.Errorf("failure must name the missing text and redact the IP: %v", err)
	}
	h.cmds.scripts = []string{"exit 7"}
	err = h.run.Step(ctx, step(t, "  - {name: e, timeout: 1s, exec: {pod: p, container: server, command: [false]}}\n"))
	if err == nil || !strings.Contains(err.Error(), "exit status 7") {
		t.Errorf("a non-zero exit must surface: %v", err)
	}
	err = h.run.Step(ctx, step(t, "  - {name: e, timeout: 1s, exec: {selector: no=pod, container: server, command: [true]}}\n"))
	if err == nil || !strings.Contains(err.Error(), "no Running pod") {
		t.Errorf("no matching pod must be reported: %v", err)
	}
}

func TestDeleteObjectAndList(t *testing.T) {
	sel := map[string]string{"valkey.io/cluster": "demo"}
	held := pod("held", corev1.PodRunning, nil)
	held.Finalizers = []string{"testbench.dev/hold"}
	h := newHarness(t, nil, pod("a", corev1.PodRunning, sel), pod("b", corev1.PodRunning, sel), held)
	ctx := context.Background()
	if err := h.run.Step(ctx, step(t, "  - {name: d, timeout: 5s, delete: {object: {apiVersion: v1, kind: Pod, name: a}}}\n")); err != nil {
		t.Fatal(err)
	}
	if err := h.run.Step(ctx, step(t, "  - {name: d, timeout: 5s, delete: {object: {apiVersion: v1, kind: Pod, name: a}}}\n")); err != nil {
		t.Errorf("deleting what is gone must be fine: %v", err)
	}
	err := h.run.Step(ctx, step(t, "  - {name: d, timeout: 1s, delete: {object: {apiVersion: v1, kind: Pod, name: held}}}\n"))
	if err == nil || !strings.Contains(err.Error(), "still being deleted") {
		t.Errorf("a finalizer must hold the wait: %v", err)
	}
	err = h.run.Step(ctx, step(t, "  - {name: d, timeout: 1s, delete: {list: {apiVersion: v1, kind: Pod, selector: none=here}}}\n"))
	if err == nil || !strings.Contains(err.Error(), "nothing matches") {
		t.Errorf("an empty delete must be an error: %v", err)
	}
	if err := h.run.Step(ctx, step(t, "  - {name: d, timeout: 5s, delete: {list: {apiVersion: v1, kind: Pod, selector: valkey.io/cluster=demo}}}\n")); err != nil {
		t.Fatal(err)
	}
	var pods corev1.PodList
	if err := h.run.Client.List(ctx, &pods, client.InNamespace(testNamespace), client.MatchingLabels(sel)); err != nil || len(pods.Items) != 0 {
		t.Errorf("%d labelled pods left, %v", len(pods.Items), err)
	}
}

func TestGoStepMergesVars(t *testing.T) {
	var got map[string]string
	steps := map[string]GoStepFunc{
		"valkey.shardRoles": func(_ context.Context, r *Run, args map[string]string) (map[string]string, error) {
			got = args
			return map[string]string{"primary": "valkey-" + args["cluster"] + "-0-0-0", "ns": r.Namespace}, nil
		},
		"valkey.broken": func(context.Context, *Run, map[string]string) (map[string]string, error) {
			return nil, errors.New("no primary yet")
		},
	}
	h := newHarness(t, steps)
	h.run.Vars["cluster"] = "demo"
	ctx := context.Background()
	st := step(t, "  - {name: g, go: {name: valkey.shardRoles, args: {cluster: '{{ .Vars.cluster }}', shard: '0'}}}\n")
	if err := h.run.Step(ctx, st); err != nil {
		t.Fatal(err)
	}
	if got["cluster"] != "demo" || got["shard"] != "0" {
		t.Errorf("args = %v", got)
	}
	if h.run.Vars["primary"] != "valkey-demo-0-0-0" || h.run.Vars["ns"] != testNamespace {
		t.Errorf("vars = %v", h.run.Vars)
	}
	env, _ := NewCELEnv()
	broken := &Scenario{Name: "demo", Path: "p", APIVersion: APIVersion, Kind: KindScenario,
		Steps: []Step{{Name: "g", Go: &GoStep{Name: "valkey.broken"}}}}
	if err := broken.Validate("valkey", map[string]bool{"valkey.broken": true}, env); err != nil {
		t.Fatal(err)
	}
	err := h.run.Step(ctx, &broken.Steps[0])
	if err == nil || !strings.Contains(err.Error(), "go step valkey.broken: no primary yet") {
		t.Errorf("go error not wrapped: %v", err)
	}
}

func TestCleanupDeletesAppliedObjectsThenTheNamespace(t *testing.T) {
	h := newHarness(t, nil)
	ctx := context.Background()
	if err := h.run.EnsureNamespace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := h.run.Step(ctx, step(t, "  - {name: apply, apply: {file: manifests/cluster.yaml}}\n")); err != nil {
		t.Fatal(err)
	}
	if err := h.run.Cleanup(ctx); err != nil {
		t.Fatal(err)
	}
	logText := h.log.String()
	cm, vc, ns := strings.Index(logText, "deleted ConfigMap notes"), strings.Index(logText, "deleted ValkeyCluster demo"), strings.Index(logText, "deleted Namespace")
	if cm < 0 || vc < 0 || ns < 0 || cm >= vc || vc >= ns {
		t.Errorf("cleanup order must be newest first, then the namespace:\n%s", logText)
	}
	var left corev1.Namespace
	if err := h.run.Client.Get(ctx, client.ObjectKey{Name: testNamespace}, &left); err == nil {
		t.Error("namespace still exists after cleanup")
	}

	kept := newHarness(t, nil)
	kept.run.Keep = true
	if err := kept.run.EnsureNamespace(ctx); err != nil {
		t.Fatal(err)
	}
	if err := kept.run.Cleanup(ctx); err != nil || !strings.Contains(kept.log.String(), "keeping namespace") {
		t.Errorf("Keep must leave everything: %v, %q", err, kept.log.String())
	}
	if err := kept.run.Client.Get(ctx, client.ObjectKey{Name: testNamespace}, &left); err != nil {
		t.Errorf("kept namespace gone: %v", err)
	}
}

func TestDeleteLabelledNamespaces(t *testing.T) {
	mine := func(name string) *corev1.Namespace {
		return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{LabelPack: "valkey"}}}
	}
	other := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "testbench-other-x", Labels: map[string]string{LabelPack: "other"}}}
	c := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(mine("testbench-valkey-a"), mine("testbench-valkey-a-2"), other).Build()
	ctx := context.Background()
	if err := DeleteLabelledNamespaces(ctx, c, "valkey", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	var list corev1.NamespaceList
	if err := c.List(ctx, &list); err != nil || len(list.Items) != 1 || list.Items[0].Name != other.Name {
		t.Errorf("namespaces left: %v, %v; want only the other pack's", list.Items, err)
	}
}

func TestSmallHelpers(t *testing.T) {
	if got := kubectlArgs("", "get", "pods"); strings.Join(got, " ") != "get pods" {
		t.Errorf("kubectlArgs without context = %v", got)
	}
	if got := kubectlExecArgs("kind-testbench", "ns", "pod", "c"); strings.Join(got, " ") != "--context kind-testbench exec -n ns pod -c c --" {
		t.Errorf("kubectlExecArgs = %v", got)
	}
	objs, err := decodeManifest([]byte("---\napiVersion: v1\nkind: ConfigMap\nmetadata: {name: a}\n---\n\n---\napiVersion: v1\nkind: Secret\nmetadata: {name: b}\n"))
	if err != nil || len(objs) != 2 || objs[1].GetKind() != "Secret" {
		t.Errorf("decodeManifest = %v, %v", objs, err)
	}
	if _, err := decodeManifest([]byte("apiVersion: v1\nkind: ConfigMap\n")); err == nil {
		t.Error("a nameless object decoded")
	}
	var ran []int
	err = RunEach(
		func() error { ran = append(ran, 1); return errors.New("first") },
		func() error { ran = append(ran, 2); return nil },
		func() error { ran = append(ran, 3); return errors.New("third") },
	)
	if len(ran) != 3 || err == nil || !strings.Contains(err.Error(), "first") || !strings.Contains(err.Error(), "third") {
		t.Errorf("RunEach ran %v with %v; want every step and both errors", ran, err)
	}
}
