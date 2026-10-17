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
)

func getenvFrom(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestParseEnvDefaults(t *testing.T) {
	e, err := ParseEnv(getenvFrom(map[string]string{EnvContext: "kind-testbench", EnvOperator: "valkey"}))
	if err != nil {
		t.Fatal(err)
	}
	want := Env{Context: "kind-testbench", Operator: "valkey", Install: InstallHelm}
	if e != want {
		t.Errorf("ParseEnv = %+v, want %+v", e, want)
	}
	e, err = ParseEnv(getenvFrom(map[string]string{
		EnvContext: "kind-testbench", EnvOperator: "valkey", EnvVersion: "v0.7.0",
		EnvInstall: InstallSkip, EnvKeep: "true", EnvForce: "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	want = Env{Context: "kind-testbench", Operator: "valkey", Version: "v0.7.0", Install: InstallSkip, Keep: true, Force: true}
	if e != want {
		t.Errorf("ParseEnv = %+v, want %+v", e, want)
	}
}

func TestParseEnvQuotesBadValues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   map[string]string
		wants []string
	}{
		{"missing operator", map[string]string{}, []string{EnvOperator + " is required"}},
		{"bad operator name", map[string]string{EnvOperator: "Val key"}, []string{`"Val key"`, "DNS label"}},
		{"version with a slash", map[string]string{EnvOperator: "valkey", EnvVersion: "tags/v1"}, []string{`"tags/v1"`}},
		{"bad install", map[string]string{EnvOperator: "valkey", EnvInstall: "kustomize"}, []string{`"kustomize"`, "helm or skip"}},
		{"keep is not a bool", map[string]string{EnvOperator: "valkey", EnvKeep: "True"}, []string{EnvKeep + `="True"`}},
		{"force is not a bool", map[string]string{EnvOperator: "valkey", EnvForce: "yes"}, []string{EnvForce + `="yes"`}},
		{"everything at once", map[string]string{EnvInstall: "x", EnvKeep: "y"}, []string{EnvOperator, EnvInstall, EnvKeep}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseEnv(getenvFrom(tc.set))
			if err == nil {
				t.Fatalf("accepted %v", tc.set)
			}
			for _, want := range tc.wants {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q lacks %q", err, want)
				}
			}
		})
	}
}
