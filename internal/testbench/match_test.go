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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func live() *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "valkey.io/v1alpha1",
		"kind":       "ValkeyCluster",
		"metadata": map[string]any{
			"name": "demo", "namespace": "testbench-valkey-demo",
			"managedFields": []any{map[string]any{"manager": "operator-testbench"}},
			"annotations":   map[string]any{"kubectl.kubernetes.io/last-applied-configuration": "{...}", "keep": "me"},
		},
		"spec": map[string]any{"shards": int64(3), "nodeName": "worker-1"},
		"status": map[string]any{
			"state": "Ready", "readyShards": int64(3), "podIP": "10.244.1.5",
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True"},
				map[string]any{"type": "Progressing", "status": "False"},
			},
		},
	}}
}

func TestCheckSubsetPinsDeepDerivative(t *testing.T) {
	for _, tc := range []struct {
		name     string
		expected map[string]any
		ok       bool
	}{
		{"subset with extra live keys", map[string]any{"status": map[string]any{"state": "Ready"}}, true},
		{"int64 against int64", map[string]any{"status": map[string]any{"readyShards": int64(3)}}, true},
		{"list as an ordered prefix", map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Ready"}}}}, true},
		{"missing key fails", map[string]any{"status": map[string]any{"reason": "ClusterHealthy"}}, false},
		{"different value fails", map[string]any{"status": map[string]any{"state": "Failed"}}, false},
		{"float against int64 fails: types must match", map[string]any{"status": map[string]any{"readyShards": float64(3)}}, false},
		{"list out of order fails", map[string]any{"status": map[string]any{"conditions": []any{map[string]any{"type": "Progressing"}}}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkSubset(tc.expected, live())
			if (err == nil) != tc.ok {
				t.Fatalf("checkSubset = %v, want ok=%t", err, tc.ok)
			}
			if err != nil {
				for _, want := range []string{"expected subset", "actual:", "state: Ready"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("message %q lacks %q", err, want)
					}
				}
			}
		})
	}
}

func TestForReportStripsNoiseAndSecrets(t *testing.T) {
	out := ForReport(live())
	for _, gone := range []string{"managedFields", "last-applied-configuration", "nodeName", "worker-1", "10.244.1.5"} {
		if strings.Contains(out, gone) {
			t.Errorf("report still contains %q:\n%s", gone, out)
		}
	}
	for _, kept := range []string{"keep: me", "state: Ready", "podIP: [redacted ip]"} {
		if !strings.Contains(out, kept) {
			t.Errorf("report lacks %q:\n%s", kept, out)
		}
	}

	secret := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "v1", "kind": "Secret",
		"metadata":   map[string]any{"name": "internal-demo-system-passwords"},
		"data":       map[string]any{"_operator": "c2VjcmV0", "_exporter": "eA=="},
		"stringData": map[string]any{"plain": "hunter2"},
	}}
	out = ForReport(secret)
	for _, gone := range []string{"c2VjcmV0", "eA==", "hunter2"} {
		if strings.Contains(out, gone) {
			t.Errorf("secret value %q leaked:\n%s", gone, out)
		}
	}
	if !strings.Contains(out, "_operator: <redacted 8 bytes>") || !strings.Contains(out, "plain: <redacted 7 bytes>") {
		t.Errorf("secret values not replaced by their length:\n%s", out)
	}

	big := live()
	big.Object["spec"].(map[string]any)["blob"] = strings.Repeat("x", 2*reportLimit)
	if out = ForReport(big); len(out) > reportLimit+32 || !strings.HasSuffix(out, "(truncated)") {
		t.Errorf("report not truncated: %d bytes, tail %q", len(out), out[max(0, len(out)-20):])
	}
}

func TestRedactLine(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"master_host:10.244.1.5 master_port:6379", "master_host:[redacted ip] master_port:6379"},
		{"dial https://user:pw@example.internal:6443/api failed", "dial [redacted url] failed"},
		{"password=hunter2 ok", "password=[redacted] ok"},
		{`"token": "abc.def"`, `"token": [redacted]`},
		{"VALKEYCLI_AUTH=s3cr3t sh -c", "VALKEYCLI_AUTH=[redacted] sh -c"},
		{"created Secret internal-demo-acl", "created Secret internal-demo-acl"},
		{"cluster_state:ok cluster_known_nodes:6 written=50 ok=500", "cluster_state:ok cluster_known_nodes:6 written=50 ok=500"},
		{"version 9.0.0 of valkey", "version 9.0.0 of valkey"},
	} {
		if got := redactLine(tc.in); got != tc.want {
			t.Errorf("redactLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
