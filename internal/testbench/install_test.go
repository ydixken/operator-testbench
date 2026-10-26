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
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const operatorNS = "testbench-valkey-system"

func controllerPod(name string, ready bool, images ...string) *corev1.Pod {
	p := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: operatorNS,
		Labels: map[string]string{"app.kubernetes.io/name": "valkey-operator", "control-plane": "controller-manager"}}}
	if len(images) == 0 {
		images = []string{"ghcr.io/valkey-io/valkey-operator:v0.7.1"}
	}
	for i, image := range images {
		p.Spec.Containers = append(p.Spec.Containers, corev1.Container{Name: fmt.Sprintf("c%d", i), Image: image})
	}
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	p.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: status}}
	return p
}

func newInstaller(t *testing.T, kubeContext string, scripts []string, objs ...client.Object) (*Installer, *fakeCommands, *bytes.Buffer) {
	t.Helper()
	pack, err := LoadPack(packFS(), "valkey", "", knownSteps)
	if err != nil {
		t.Fatal(err)
	}
	cmds := &fakeCommands{scripts: scripts}
	log := &bytes.Buffer{}
	c := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(objs...).Build()
	return &Installer{Pack: pack, Context: kubeContext, Client: c, Command: cmds.factory, Log: log, ReadyTimeout: time.Second}, cmds, log
}

func TestInstallerCommandLines(t *testing.T) {
	ctx := context.Background()
	inst, cmds, _ := newInstaller(t, "kind-testbench", []string{"true"})
	if _, err := inst.ReleaseExists(ctx); err != nil {
		t.Fatal(err)
	}
	if err := inst.ApplyCRDs(ctx, false); err != nil {
		t.Fatal(err)
	}
	if err := inst.HelmInstall(ctx); err != nil {
		t.Fatal(err)
	}
	if err := inst.HelmUninstall(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"helm --kube-context kind-testbench status testbench-valkey-operator -n " + operatorNS,
		"kubectl --context kind-testbench get -f " +
			"https://raw.githubusercontent.com/valkey-io/valkey-operator/v0.7.1/config/crd/bases/valkey.io_valkeyclusters.yaml" +
			" --ignore-not-found --no-headers -o custom-columns=NAME:.metadata.name,MANAGERS:.metadata.managedFields[*].manager",
		"kubectl --context kind-testbench apply --server-side --force-conflicts --field-manager operator-testbench -f " +
			"https://raw.githubusercontent.com/valkey-io/valkey-operator/v0.7.1/config/crd/bases/valkey.io_valkeyclusters.yaml",
		"helm --kube-context kind-testbench install testbench-valkey-operator valkey-operator --repo https://valkey.io/valkey-helm" +
			" --version 0.7.0 -n " + operatorNS + " --skip-crds --wait --timeout 5m --set image.tag=v0.7.1" +
			" --set manager.watchNamespaces={testbench-valkey-alpha,testbench-valkey-cluster-ready}",
		"helm --kube-context kind-testbench uninstall testbench-valkey-operator -n " + operatorNS + " --ignore-not-found --wait",
	}
	calls := cmds.calls()
	if len(calls) != len(want) {
		t.Fatalf("%d commands, want %d: %q", len(calls), len(want), calls)
	}
	for i := range want {
		if got := strings.Join(calls[i], " "); got != want[i] {
			t.Errorf("command %d\n got %s\nwant %s", i+1, got, want[i])
		}
	}

	inst, cmds, _ = newInstaller(t, "", []string{"true"})
	if _, err := inst.ReleaseExists(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cmds.calls()[0], " "); strings.Contains(got, "--kube-context") {
		t.Errorf("in-cluster run must pass no context: %s", got)
	}
}

func TestInstallerRunRetries(t *testing.T) {
	ctx := context.Background()
	inst, cmds, log := newInstaller(t, "", []string{"echo first >&2; exit 1", "exit 1", "echo installed"})
	if err := inst.HelmInstall(ctx); err != nil {
		t.Fatalf("third attempt succeeds, so the install must: %v", err)
	}
	if n := len(cmds.calls()); n != 3 {
		t.Errorf("%d attempts, want 3", n)
	}
	if !strings.Contains(log.String(), "retrying") || !strings.Contains(log.String(), "installed") {
		t.Errorf("log lacks the retry and the output:\n%s", log.String())
	}
	inst, cmds, _ = newInstaller(t, "", []string{"echo 'Error: cannot reach https://valkey.io/valkey-helm'; exit 2"})
	err := inst.HelmInstall(ctx)
	if err == nil || !strings.Contains(err.Error(), "exit status 2") || !strings.Contains(err.Error(), "helm install") {
		t.Errorf("final error must name the command and the exit: %v", err)
	}
	if strings.Contains(err.Error(), "valkey.io/valkey-helm'") {
		t.Errorf("error output must be redacted: %v", err)
	}
	if n := len(cmds.calls()); n != commandAttempts {
		t.Errorf("%d attempts, want %d", n, commandAttempts)
	}
}

func TestReleaseExists(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		script string
		exists bool
		fail   bool
	}{
		{"exit 0", true, false},
		{"echo 'Error: release: not found'; exit 1", false, false},
		{"echo 'Kubernetes cluster unreachable'; exit 1", false, true},
	} {
		inst, _, _ := newInstaller(t, "", []string{tc.script})
		exists, err := inst.ReleaseExists(ctx)
		if exists != tc.exists || (err != nil) != tc.fail {
			t.Errorf("%q: exists=%t err=%v; want %t, fail=%t", tc.script, exists, err, tc.exists, tc.fail)
		}
	}
}

func TestInstallHonoursTheLock(t *testing.T) {
	ctx := context.Background()
	// helm status succeeds: another run holds the release.
	inst, cmds, log := newInstaller(t, "", []string{"exit 0"}, controllerPod("ctl", true))
	err := inst.Install(ctx, false)
	if !errors.Is(err, ErrReleaseHeld) || !strings.Contains(err.Error(), EnvForce+"=true") {
		t.Fatalf("Install without force = %v, want ErrReleaseHeld naming the knob", err)
	}
	if n := len(cmds.calls()); n != 1 {
		t.Errorf("%d commands after a refused install, want only the status check", n)
	}
	if err := inst.Install(ctx, true); err != nil {
		t.Fatalf("Install with force: %v", err)
	}
	verbs := make([]string, 0, len(cmds.calls())-1)
	for _, c := range cmds.calls()[1:] {
		verbs = append(verbs, c[0]+" "+c[1])
	}
	if got := strings.Join(verbs, ", "); got != "helm status, helm uninstall, kubectl get, kubectl apply, helm install" {
		t.Errorf("force path ran %s", got)
	}
	if !strings.Contains(log.String(), "taking over release") {
		t.Errorf("log does not say the release was taken over:\n%s", log.String())
	}
	var ns corev1.Namespace
	if err := inst.Client.Get(ctx, client.ObjectKey{Name: operatorNS}, &ns); err != nil || ns.Labels[LabelManagedBy] != FieldOwner {
		t.Errorf("operator namespace not created with the engine's label: %v %v", err, ns.Labels)
	}
}

func TestApplyCRDsRefusesForeignCRDs(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name, get string
		force     bool
		applied   bool
		want      string
	}{
		{"absent", "true", false, true, ""},
		{"own", "echo 'valkeyclusters.valkey.io   helm,operator-testbench'", false, true, ""},
		{"foreign", "echo 'valkeyclusters.valkey.io   helm'", false, false, "CRD valkeyclusters.valkey.io exists"},
		{"no managers", "echo 'valkeyclusters.valkey.io   <none>'", false, false, "field managers <none>"},
		{"foreign forced", "echo 'valkeyclusters.valkey.io   helm'", true, true, "replacing CRD valkeyclusters.valkey.io"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, cmds, log := newInstaller(t, "", []string{tc.get, "true"})
			err := inst.ApplyCRDs(ctx, tc.force)
			if applied := len(cmds.calls()) == 2; applied != tc.applied {
				t.Errorf("applied = %t, want %t (err %v)", applied, tc.applied, err)
			}
			switch {
			case tc.applied && err != nil:
				t.Errorf("ApplyCRDs = %v", err)
			case !tc.applied && (err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), EnvForce)):
				t.Errorf("ApplyCRDs = %v, want an error containing %q and naming %s", err, tc.want, EnvForce)
			case tc.applied && !strings.Contains(log.String(), tc.want):
				t.Errorf("log lacks %q:\n%s", tc.want, log.String())
			}
		})
	}
}

func TestHelmInstallWatchesEveryProcess(t *testing.T) {
	inst, cmds, _ := newInstaller(t, "", []string{"true"})
	inst.Procs = 2
	if err := inst.HelmInstall(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := "--set manager.watchNamespaces={testbench-valkey-alpha,testbench-valkey-cluster-ready," +
		"testbench-valkey-alpha-2,testbench-valkey-cluster-ready-2}"
	if got := strings.Join(cmds.calls()[0], " "); !strings.HasSuffix(got, want) {
		t.Errorf("helm install\n got %s\nwant suffix %s", got, want)
	}
}

func TestWaitReady(t *testing.T) {
	ctx := context.Background()
	inst, _, _ := newInstaller(t, "", []string{"true"}, controllerPod("ctl", false))
	err := inst.WaitReady(ctx)
	if err == nil || !strings.Contains(err.Error(), "no Ready pod matches") || !strings.Contains(err.Error(), "(1 pods)") {
		t.Errorf("WaitReady with an unready pod = %v", err)
	}
	inst, _, _ = newInstaller(t, "", []string{"true"}, controllerPod("ctl", false), controllerPod("ctl-2", true))
	if err := inst.WaitReady(ctx); err != nil {
		t.Errorf("WaitReady with a Ready pod = %v", err)
	}
}

func TestImageTag(t *testing.T) {
	for image, want := range map[string]string{
		"ghcr.io/valkey-io/valkey-operator:v0.7.1":                  "v0.7.1",
		"registry.local:5000/valkey-operator:0.7.1":                 "0.7.1",
		"registry.local:5000/valkey-operator":                       "",
		"valkey-operator:v0.7.1@sha256:0123456789abcdef":            "v0.7.1",
		"ghcr.io/valkey-io/valkey-operator@sha256:0123456789abcdef": "",
	} {
		if got := imageTag(image); got != want {
			t.Errorf("imageTag(%s) = %q, want %q", image, got, want)
		}
	}
}

func TestWaitReadyChecksTheVersion(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		container string
		pods      []client.Object
		want      string // empty: passes
	}{
		{"match without v", "", []client.Object{controllerPod("ctl", true, "r.io/op:0.7.1")}, ""},
		{"unready pod ignored", "", []client.Object{controllerPod("ctl", true), controllerPod("old", false, "r.io/op:v0.6.0")}, ""},
		{"mismatch", "", []client.Object{controllerPod("ctl", true, "r.io/op:v0.7.0")}, "ctl runs v0.7.0"},
		{"mixed tags", "", []client.Object{controllerPod("ctl", true), controllerPod("old", true, "r.io/op:v0.6.0")}, "old runs v0.6.0"},
		{"digest only", "", []client.Object{controllerPod("ctl", true, "r.io/op@sha256:00")}, "without a tag"},
		{"two containers", "", []client.Object{controllerPod("ctl", true, "r.io/op:v0.7.1", "r.io/proxy:v1")}, "name the controller"},
		{"named container", "c1", []client.Object{controllerPod("ctl", true, "r.io/proxy:v1", "r.io/op:v0.7.1")}, ""},
		{"named container missing", "manager", []client.Object{controllerPod("ctl", true)}, "no container manager"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inst, _, _ := newInstaller(t, "", []string{"true"}, tc.pods...)
			inst.Pack.Spec.Operator.Container = tc.container
			err := inst.WaitReady(ctx)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("WaitReady = %v", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Errorf("WaitReady = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestUninstallDeletesOnlyItsOwnNamespace(t *testing.T) {
	ctx := context.Background()
	mine := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorNS, Labels: map[string]string{LabelManagedBy: FieldOwner}}}
	inst, cmds, _ := newInstaller(t, "", []string{"true"}, mine)
	if err := inst.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cmds.calls()[0], " "); !strings.HasPrefix(got, "helm uninstall") {
		t.Errorf("Uninstall ran %s first, want helm uninstall", got)
	}
	if err := inst.Client.Get(ctx, client.ObjectKey{Name: operatorNS}, &corev1.Namespace{}); err == nil {
		t.Error("the engine's own namespace survived Uninstall")
	}

	theirs := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: operatorNS}}
	inst, _, log := newInstaller(t, "", []string{"true"}, theirs)
	if err := inst.Uninstall(ctx); err != nil {
		t.Fatal(err)
	}
	if err := inst.Client.Get(ctx, client.ObjectKey{Name: operatorNS}, &corev1.Namespace{}); err != nil {
		t.Error("a namespace the engine did not create was deleted")
	}
	if !strings.Contains(log.String(), "leaving it") {
		t.Errorf("log does not explain the namespace was left:\n%s", log.String())
	}
}
