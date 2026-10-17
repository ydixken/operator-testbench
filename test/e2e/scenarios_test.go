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
	"fmt"
	"os/exec"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/ydixken/operator-testbench/internal/testbench"
)

// commandContext is the production command factory.
func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// describeScenarios turns the loaded pack into the spec tree: one top-level
// Ordered container per scenario, so SpecPriority is honoured and a failed step
// skips the rest of its own scenario only, and one It per step. The tree is
// built at package init from embedded files in file order, so every parallel
// process builds the same one; nothing here reads the cluster.
func describeScenarios(s *suiteState) bool {
	if s == nil {
		return false
	}
	for i := range s.Pack.Scenarios {
		sc := &s.Pack.Scenarios[i]
		labels := append(slices.Clone(sc.Labels), s.Pack.Name)
		Describe(sc.Name, Ordered, Label(labels...), SpecPriority(sc.Priority), func() {
			var run *testbench.Run
			diag := &testbench.Diagnostics{Context: kubeContextName(), Command: commandContext, Pack: s.Pack}

			BeforeAll(func(ctx SpecContext) {
				run = testbench.NewRun(k8sClient, kubeContextName(), s.Pack, sc, GinkgoParallelProcess(), s.Env,
					goSteps, commandContext, GinkgoWriter)
				diag.Client = k8sClient
				// Registered before the namespace exists, so a failed create still cleans up.
				DeferCleanup(func(ctx SpecContext) {
					Expect(run.Cleanup(ctx)).To(Succeed(), "cleanup of %s left state behind", run.Namespace)
				}, NodeTimeout(teardownTimeout))
				Expect(run.EnsureNamespace(ctx)).To(Succeed())
			}, NodeTimeout(testbench.DefaultStepTimeout))

			// Diagnostics attach to the failed step's report; every read reports
			// and never asserts, so a reaped pod cannot replace the real failure.
			AfterEach(func(ctx SpecContext) {
				if run == nil || !CurrentSpecReport().Failed() {
					return
				}
				AddReportEntry("namespace "+run.Namespace, diag.Namespace(ctx, run.Namespace), ReportEntryVisibilityFailureOrVerbose)
				AddReportEntry("controller logs", diag.ControllerLogs(ctx, 60), ReportEntryVisibilityFailureOrVerbose)
			}, NodeTimeout(testbench.DefaultStepTimeout))

			for j := range sc.Steps {
				st := &sc.Steps[j]
				It(fmt.Sprintf("%02d %s", j+1, st.Name), func(ctx SpecContext) {
					Expect(run.Step(ctx, st)).To(Succeed())
				}, NodeTimeout(testbench.StepTimeout(st)+testbench.NodeTimeoutGrace))
			}
		})
	}
	return true
}
