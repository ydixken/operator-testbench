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
	"context"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestEventTimeFallsBackToEventTime(t *testing.T) {
	last := time.Date(2026, 10, 18, 1, 2, 3, 0, time.UTC)
	e := corev1.Event{LastTimestamp: metav1.NewTime(last), EventTime: metav1.NewMicroTime(last.Add(time.Hour))}
	if got := eventTime(e); !got.Equal(last) {
		t.Errorf("eventTime with LastTimestamp = %s", got)
	}
	e = corev1.Event{EventTime: metav1.NewMicroTime(last.Add(time.Hour))}
	if got := eventTime(e); !got.Equal(last.Add(time.Hour)) {
		t.Errorf("eventTime without LastTimestamp = %s, want EventTime", got)
	}
}

func TestNamespaceReport(t *testing.T) {
	const ns = "testbench-valkey-demo"
	running := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "valkey-demo-0-0-0", Namespace: ns},
		Spec: corev1.PodSpec{NodeName: "worker-1"},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.244.1.5", ContainerStatuses: []corev1.ContainerStatus{
			{Name: "server", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}}}}}
	crashing := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "valkey-demo-1-0-0", Namespace: ns},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, ContainerStatuses: []corev1.ContainerStatus{
			{Name: "server", RestartCount: 3, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
			{Name: "metrics-exporter", State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "Error", ExitCode: 137}}},
		}}}
	t0 := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	newer := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e-newer", Namespace: ns}, Type: "Warning", Reason: "BackOff",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "valkey-demo-1-0-0"},
		Message:        "Back-off restarting failed container at 10.244.1.5", EventTime: metav1.NewMicroTime(t0.Add(time.Minute))}
	older := &corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "e-older", Namespace: ns}, Type: "Normal", Reason: "Scheduled",
		InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "valkey-demo-0-0-0"},
		Message:        "Successfully assigned to worker-1", LastTimestamp: metav1.NewTime(t0)}
	c := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(running, crashing, newer, older).Build()
	d := &Diagnostics{Client: c}
	out := d.Namespace(context.Background(), ns)
	for _, want := range []string{"pods in " + ns + ": 2", "valkey-demo-0-0-0 Running restarts=0",
		"valkey-demo-1-0-0 Running restarts=3 (server waiting CrashLoopBackOff; metrics-exporter terminated Error exit 137)",
		"events: 2", "12:00:00 Normal Scheduled Pod/valkey-demo-0-0-0", "12:01:00 Warning BackOff Pod/valkey-demo-1-0-0", "[redacted ip]"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Index(out, " Normal Scheduled ") > strings.Index(out, " Warning BackOff ") {
		t.Errorf("events are not in time order:\n%s", out)
	}
	if strings.Contains(out, "10.244.1.5") {
		t.Errorf("report leaks an IP:\n%s", out)
	}
	if strings.Contains(out, "nodeName") {
		t.Errorf("report prints the pod spec:\n%s", out)
	}
}

func TestControllerLogs(t *testing.T) {
	pack, err := LoadPack(packFS(), "valkey", "", knownSteps)
	if err != nil {
		t.Fatal(err)
	}
	cmds := &fakeCommands{scripts: []string{"echo '[pod/ctl/manager] reconciled demo at 10.244.0.3'"}}
	d := &Diagnostics{Context: "kind-testbench", Command: cmds.factory, Pack: pack}
	out := d.ControllerLogs(context.Background(), 60)
	want := "kubectl --context kind-testbench logs -n " + operatorNS + " -l " + pack.Spec.Operator.Selector +
		" --all-containers --prefix --tail=60 --limit-bytes=65536"
	if got := strings.Join(cmds.calls()[0], " "); got != want {
		t.Errorf("argv\n got %s\nwant %s", got, want)
	}
	if !strings.Contains(out, "reconciled demo at [redacted ip]") {
		t.Errorf("logs not redacted: %q", out)
	}
	cmds.scripts = []string{"echo 'error: no pods'; exit 1"}
	if out = d.ControllerLogs(context.Background(), 60); !strings.Contains(out, "kubectl logs failed") {
		t.Errorf("a failed kubectl logs must be reported, not hidden: %q", out)
	}
}
