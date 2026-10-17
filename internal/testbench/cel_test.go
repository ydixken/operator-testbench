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
)

// A ValkeyCluster as the API server would hand it back: int64 for integers,
// nested maps and lists of maps for conditions.
func sampleObject() map[string]any {
	return map[string]any{
		"metadata": map[string]any{"name": "demo", "generation": int64(2)},
		"status": map[string]any{
			"state":       "Ready",
			"readyShards": int64(3),
			"conditions": []any{
				map[string]any{"type": "Ready", "status": "True", "observedGeneration": int64(2)},
				map[string]any{"type": "Progressing", "status": "False", "observedGeneration": int64(2)},
			},
		},
		"data": map[string]any{"valkey.conf": "dir /data\ncluster-config-file /data/nodes.conf", "users.acl": "x"},
	}
}

// node is one ValkeyNode-shaped item: a shard label and a role.
func node(shard, role string) map[string]any {
	return map[string]any{
		"metadata": map[string]any{"labels": map[string]any{"shard": shard}},
		"status":   map[string]any{"ready": true, "role": role},
	}
}

func sampleItems() []any {
	return []any{node("0", "primary"), node("0", "replica"), node("1", "primary")}
}

func TestCompileBoolAcceptsTheScenarioDialect(t *testing.T) {
	env, err := NewCELEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		expr  string
		self  any
		items []any
		want  string // "" passes, otherwise a substring of the error
	}{
		{name: "field equality", expr: "self.status.state == 'Ready'", self: sampleObject()},
		{name: "int64 against a literal", expr: "self.status.readyShards == 3", self: sampleObject()},
		{name: "cross-type comparison", expr: "self.status.readyShards >= 2.5", self: sampleObject()},
		{name: "condition by type", expr: "self.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True' && c.observedGeneration == self.metadata.generation)", self: sampleObject()},
		{name: "no degraded condition", expr: "self.status.conditions.filter(c, c.type == 'Degraded').size() == 0", self: sampleObject()},
		{name: "map key presence", expr: "'users.acl' in self.data", self: sampleObject()},
		{name: "string contains", expr: "self.data['valkey.conf'].contains('dir /data')", self: sampleObject()},
		{name: "regex", expr: "self.metadata.name.matches('^de.o$')", self: sampleObject()},
		{name: "quantity library", expr: "quantity('1Gi').isGreaterThan(quantity('512Mi'))", self: sampleObject()},
		{name: "list all", expr: "items.all(n, has(n.status.ready) && n.status.ready)", items: sampleItems()},
		{name: "one primary per shard", expr: "items.all(n, items.filter(m, m.metadata.labels['shard'] == n.metadata.labels['shard'] && m.status.role == 'primary').size() == 1)", items: sampleItems()},
		{name: "false is an error", expr: "self.status.state == 'Failed'", self: sampleObject(), want: "is false"},
		{name: "missing key is an error, not a panic", expr: "self.status.missing == 1", self: sampleObject(), want: "no such key"},
		{name: "list expression without items", expr: "items.size() == 3", self: sampleObject(), want: "items"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prg, err := env.CompileBool(tc.expr)
			if err != nil {
				t.Fatalf("CompileBool(%q): %v", tc.expr, err)
			}
			err = prg.Check(context.Background(), tc.self, tc.items)
			switch {
			case tc.want == "" && err != nil:
				t.Errorf("Check(%q) = %v, want nil", tc.expr, err)
			case tc.want != "" && err == nil:
				t.Errorf("Check(%q) = nil, want an error containing %q", tc.expr, tc.want)
			case tc.want != "" && !strings.Contains(err.Error(), tc.want):
				t.Errorf("Check(%q) = %v, want it to contain %q", tc.expr, err, tc.want)
			}
			if err != nil && !strings.Contains(err.Error(), tc.expr) {
				t.Errorf("error %q does not quote the expression", err)
			}
		})
	}
}

func TestCompileBoolRejectsWhatIsNotABoolExpression(t *testing.T) {
	env, err := NewCELEnv()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ expr, want string }{
		{expr: "self.status.state", want: "must evaluate to bool"},
		{expr: "'Ready'", want: "must evaluate to bool"},
		{expr: "nosuchfunction(self)", want: "undeclared reference"},
		{expr: "self.status.state ==", want: "Syntax error"},
		{expr: "other.status.state == 'Ready'", want: "undeclared reference"},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			_, err := env.CompileBool(tc.expr)
			if err == nil {
				t.Fatalf("CompileBool(%q) = nil, want an error containing %q", tc.expr, tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), tc.expr) {
				t.Errorf("CompileBool(%q) = %v, want it to contain %q and the expression", tc.expr, err, tc.want)
			}
		})
	}
}
