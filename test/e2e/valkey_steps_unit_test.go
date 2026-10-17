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
	"context"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/ydixken/operator-testbench/internal/testbench"
)

// shardPod is a Running pod of shard 0 of the cluster demo.
func shardPod(name string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "testbench-valkey-failover",
			Labels: map[string]string{"valkey.io/cluster": "demo", "valkey.io/shard-index": "0"}},
		Status: corev1.PodStatus{Phase: corev1.PodRunning},
	}
}

// infoBy answers kubectl exec with the INFO replication text for the pod
// named in the argv, so the fake knows nothing about call order.
func infoBy(answers map[string]string) testbench.CommandFactory {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		for _, a := range args {
			if text, ok := answers[a]; ok {
				return exec.CommandContext(ctx, "sh", "-c", "printf '%s\\n' '"+text+"'")
			}
		}
		return exec.CommandContext(ctx, "sh", "-c", "exit 1")
	}
}

func shardRun(cmd testbench.CommandFactory, objs ...client.Object) *testbench.Run {
	c := fake.NewClientBuilder().WithScheme(clientgoscheme.Scheme).WithObjects(objs...).Build()
	pack := &testbench.Pack{Name: "valkey", Version: "v0.7.1"}
	return testbench.NewRun(c, "", pack, &testbench.Scenario{Name: "failover"}, 1, testbench.Env{}, goSteps, cmd, io.Discard)
}

func TestValkeyShardRolesReadsLiveRoles(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	answers := map[string]string{
		"valkey-demo-0-0-0": "role:slave\nmaster_host:10.244.1.5\nmaster_link_status:up",
		"valkey-demo-0-1-0": "role:master\nconnected_slaves:1",
	}
	r := shardRun(infoBy(answers), shardPod("valkey-demo-0-0-0"), shardPod("valkey-demo-0-1-0"))
	got, err := valkeyShardRoles(ctx, r, map[string]string{"cluster": "demo", "shard": "0"})
	if err != nil {
		t.Fatal(err)
	}
	if got["primary"] != "valkey-demo-0-1-0" || got["replica"] != "valkey-demo-0-0-0" {
		t.Errorf("roles = %v; the labels say node 0 is primary, INFO says otherwise and wins", got)
	}

	single := shardRun(infoBy(map[string]string{"valkey-demo-0-0-0": "role:master"}), shardPod("valkey-demo-0-0-0"))
	if got, err = valkeyShardRoles(ctx, single, map[string]string{"cluster": "demo", "shard": "0"}); err != nil || got["replica"] != "" {
		t.Errorf("a single-pod shard = %v, %v; want an empty replica", got, err)
	}

	if _, err := valkeyShardRoles(ctx, r, map[string]string{"cluster": "demo"}); err == nil || !strings.Contains(err.Error(), "shard") {
		t.Errorf("missing arg = %v", err)
	}
}

func TestValkeyShardRolesWaitsForTheLink(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	answers := map[string]string{
		"valkey-demo-0-0-0": "role:master",
		"valkey-demo-0-1-0": "role:slave\nmaster_link_status:down",
	}
	r := shardRun(infoBy(answers), shardPod("valkey-demo-0-0-0"), shardPod("valkey-demo-0-1-0"))
	_, err := valkeyShardRoles(ctx, r, map[string]string{"cluster": "demo", "shard": "0"})
	if err == nil || !strings.Contains(err.Error(), "link up") || strings.Contains(err.Error(), "10.244") {
		t.Errorf("a syncing replica must not be accepted, and the error must name no address: %v", err)
	}
}
