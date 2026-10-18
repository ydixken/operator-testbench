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

// The Taskfile and the workflows carry the safety rules that keep this suite
// off clusters nobody named and keep the merge gate honest. These tests pin
// them, so a refactor cannot drop one with every other test green.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/yaml"
)

const (
	taskfile        = "../../Taskfile.yml"
	ciWorkflow      = "../../.github/workflows/ci.yml"
	e2eKindWorkflow = "../../.github/workflows/e2e-kind.yml"
	nightlyWorkflow = "../../.github/workflows/nightly.yml"
	workflowDir     = "../../.github/workflows"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

type workflowStep struct {
	Name string            `json:"name"`
	Uses string            `json:"uses"`
	Run  string            `json:"run"`
	Env  map[string]string `json:"env"`
}

type workflowJob struct {
	Name   string         `json:"name"`
	RunsOn string         `json:"runs-on"`
	Steps  []workflowStep `json:"steps"`
}

type workflow struct {
	Jobs map[string]workflowJob `json:"jobs"`
}

func mustParse(t *testing.T, path string) workflow {
	t.Helper()
	var wf workflow
	if err := yaml.Unmarshal([]byte(read(t, path)), &wf); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return wf
}

// A task that loses TESTBENCH_CONTEXT skips TestE2E, and the skip exits 0, so
// the target reports green after running no specs. Task's env: block loses to
// an exported value, so only an assignment in the command binds the confirmed
// context.
func TestE2ETasksNameTheirContext(t *testing.T) {
	const prefix = "TESTBENCH_CONTEXT='{{.KUBE_CONTEXT}}' "
	var tf struct {
		Tasks map[string]struct {
			Cmds []any `json:"cmds"`
		} `json:"tasks"`
	}
	if err := yaml.Unmarshal([]byte(read(t, taskfile)), &tf); err != nil {
		t.Fatalf("parse Taskfile.yml: %v", err)
	}
	var runners []string
	for name, task := range tf.Tasks {
		for _, c := range task.Cmds {
			s := fmt.Sprint(c)
			if !strings.Contains(s, "go test ./test/e2e") && !strings.Contains(s, "ginkgo/v2/ginkgo") {
				continue
			}
			runners = append(runners, name)
			if !strings.HasPrefix(s, prefix) {
				t.Errorf("task %s runs the e2e suite with %q, want it to start with %q", name, s, prefix)
			}
		}
	}
	for _, want := range []string{"e2e:run", "e2e:run:parallel"} {
		if !slices.Contains(runners, want) {
			t.Errorf("task %s no longer runs the e2e suite; update this test's command patterns", want)
		}
	}
}

// A -skip regex wider than the cluster entry point would drop tests unnoticed.
// -run allowlists are refused for the same reason: a new test is picked up
// without touching the workflow.
func TestCIRunsEveryClusterFreeTest(t *testing.T) {
	test, ok := mustParse(t, ciWorkflow).Jobs["test"]
	if !ok {
		t.Fatal("ci.yml has no test job")
	}
	const namePrefix = "go test ./... -skip '"
	var skip string
	steps := 0
	for _, step := range test.Steps {
		if !strings.Contains(step.Run, "go test ") {
			continue
		}
		start := strings.Index(step.Run, namePrefix)
		if start < 0 {
			t.Errorf("test step must select tests with -skip over ./..., not -run: %q", step.Run)
			continue
		}
		rest := step.Run[start+len(namePrefix):]
		end := strings.IndexByte(rest, '\'')
		if end < 0 {
			t.Fatalf("test step has no closing quote after the -skip regex: %q", step.Run)
		}
		for _, flag := range []string{"-race", "-count=1"} {
			if !slices.Contains(strings.Fields(rest[end+1:]), flag) {
				t.Errorf("test step must include %s: %q", flag, step.Run)
			}
		}
		steps++
		skip = rest[:end]
	}
	if steps != 1 {
		t.Fatalf("ci.yml test job contains %d `go test` steps, want 1", steps)
	}
	re, err := regexp.Compile(skip)
	if err != nil {
		t.Fatalf("-skip regex %q does not compile: %v", skip, err)
	}
	if !re.MatchString("TestE2E") {
		t.Errorf("-skip regex %q does not skip TestE2E", skip)
	}
	for _, name := range []string{"TestE2EContextGate", "TestE2ETasksNameTheirContext", "TestEmbeddedPacksParse"} {
		if re.MatchString(name) {
			t.Errorf("-skip regex %q also skips %s", skip, name)
		}
	}
}

// Branch protection requires the checks by name, and GitHub reports an
// Actions job under its id when it has no name: key. A renamed job would
// leave the protection waiting for a check that never reports.
func TestRequiredCheckJobsKeepTheirIDs(t *testing.T) {
	wf := mustParse(t, ciWorkflow)
	for _, id := range []string{"lint", "test", "docs"} {
		job, ok := wf.Jobs[id]
		if !ok {
			t.Errorf("ci.yml has no %s job; branch protection requires that check", id)
			continue
		}
		if job.Name != "" {
			t.Errorf("ci.yml job %s sets name %q; the required check is the job id", id, job.Name)
		}
		if job.RunsOn != "ubuntu-latest" {
			t.Errorf("ci.yml job %s runs on %q, want ubuntu-latest: no self-hosted runners in a public repo", id, job.RunsOn)
		}
	}
}

// TestE2E skips without TESTBENCH_CONTEXT, and a skipped suite exits 0, so a
// kind workflow would pass without running a spec. Every ginkgo step therefore
// refuses an empty context, fails on an empty spec set, keeps the package's
// cluster-free tests out of every process, and runs the label filter its
// workflow exists for.
func TestKindWorkflowsNameTheirContext(t *testing.T) {
	for _, tc := range []struct{ path, job, filter string }{
		{e2eKindWorkflow, "e2e-kind", "--label-filter='!chaos'"},
		{nightlyWorkflow, "chaos", "--label-filter='chaos'"},
	} {
		job, ok := mustParse(t, tc.path).Jobs[tc.job]
		if !ok {
			t.Errorf("%s has no %s job", tc.path, tc.job)
			continue
		}
		if job.Name != "" || job.RunsOn != "ubuntu-latest" {
			t.Errorf("%s job %s must keep its id as its name and run on ubuntu-latest, got name %q on %q", tc.path, tc.job, job.Name, job.RunsOn)
		}
		runs := 0
		for _, step := range job.Steps {
			if !strings.Contains(step.Run, "ginkgo/v2/ginkgo") {
				continue
			}
			runs++
			if step.Env["TESTBENCH_CONTEXT"] != "kind-testbench" {
				t.Errorf("%s run step sets TESTBENCH_CONTEXT=%q, want kind-testbench", tc.path, step.Env["TESTBENCH_CONTEXT"])
			}
			for _, want := range []string{`: "${TESTBENCH_CONTEXT:?`, "--fail-on-empty", "-- -test.run='^TestE2E$'", tc.filter} {
				if !strings.Contains(step.Run, want) {
					t.Errorf("%s run step lacks %q:\n%s", tc.path, want, step.Run)
				}
			}
		}
		if runs != 1 {
			t.Errorf("%s job %s has %d ginkgo steps, want 1", tc.path, tc.job, runs)
		}
	}
	if !strings.Contains(read(t, e2eKindWorkflow), "pull_request:") {
		t.Error("e2e-kind.yml must run on pull_request; it is the merge gate's cluster coverage")
	}
}

// pull_request_target runs the base branch's workflow with its secrets against
// a fork's code, so the approval that gates a fork's first run is never asked for.
func TestNoWorkflowUsesPullRequestTarget(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(workflowDir, "*.yml"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no workflows under %s: %v", workflowDir, err)
	}
	for _, path := range paths {
		if strings.Contains(read(t, path), "pull_request_target") {
			t.Errorf("%s uses pull_request_target", path)
		}
	}
}

const e2eSuite = "e2e_suite_test.go"

// The suite reads the current context to compare it with TESTBENCH_CONTEXT;
// the gate's own test keeps it out of error messages, and this keeps it out
// of a banner, because CI logs are public.
func TestE2ESuiteDoesNotPrintTheRawKubeContext(t *testing.T) {
	for _, phrase := range []string{"running against kubectl context", "suite.Env.Context)"} {
		if strings.Contains(read(t, e2eSuite), "GinkgoWriter, "+phrase) || strings.Contains(read(t, e2eSuite), "By(\"context ") {
			t.Errorf("e2e suite prints the kube context (%q), and CI logs are public", phrase)
		}
	}
}

// TestE2EContextGate covers the decision, not the call, so a refactor could
// drop the gate from TestE2E with every unit test green.
func TestE2ECallsTheContextGateBeforeRunSpecs(t *testing.T) {
	const head = "func TestE2E(t *testing.T) {"
	src := read(t, e2eSuite)
	start := strings.Index(src, head)
	if start < 0 {
		t.Fatalf("%s has no %q", e2eSuite, head)
	}
	body, _, _ := strings.Cut(src[start:], "\n}\n")
	gate := strings.Index(body, "e2eContextGate(os.Getenv(contextEnv), kubeCurrentContext)")
	run := strings.Index(body, "RunSpecs(")
	if gate < 0 || run < 0 || gate > run {
		t.Error("TestE2E must call e2eContextGate before RunSpecs, or a plain go test reaches the current context")
	}
	if !strings.Contains(body, "suiteConfig.FailOnEmpty = true") {
		t.Error("TestE2E must set FailOnEmpty, or a mistyped label filter passes a run of nothing")
	}
}
