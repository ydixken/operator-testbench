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

// Package e2e runs an operator pack's scenarios on the cluster behind the
// current kubectl context, once TESTBENCH_CONTEXT names that context. The
// suite installs the pack's operator into the pack's own namespace, runs every
// scenario in a namespace of its own, and removes both again. Outside those
// namespaces it applies only the pack's CRDs, and it refuses CRDs another
// install of the operator owns unless TESTBENCH_FORCE is set.
package e2e

import (
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/ydixken/operator-testbench/internal/testbench"
	"github.com/ydixken/operator-testbench/operators"
)

// teardownTimeout bounds the suite's own teardown node.
const teardownTimeout = 10 * time.Minute

// suiteState is what the spec tree is built from. It is loaded at package
// init, before Ginkgo constructs the tree, and only when TESTBENCH_CONTEXT is
// set: a plain `go test ./...` reads no pack and builds no spec.
type suiteState struct {
	Env  testbench.Env
	Pack *testbench.Pack
}

// loadSuite reads the knobs and the pack. (nil, nil) means the gate is shut.
// Every Ginkgo process runs this and gets the same tree, because the pack is
// embedded and its scenarios are read in file order.
func loadSuite(getenv func(string) string) (*suiteState, error) {
	if getenv(testbench.EnvContext) == "" {
		return nil, nil
	}
	env, err := testbench.ParseEnv(getenv)
	if err != nil {
		return nil, err
	}
	pack, err := testbench.LoadPack(operators.FS, env.Operator, env.Version, goStepNames())
	if err != nil {
		return nil, err
	}
	return &suiteState{Env: env, Pack: pack}, nil
}

var (
	suite, suiteErr = loadSuite(os.Getenv)
	_               = describeScenarios(suite)

	k8sClient client.Client
)

// TestE2E is the cluster entry point. The gate runs before RunSpecs: Ginkgo
// runs the suite teardown for any attempted setup, so refusing inside the
// suite would be too late.
func TestE2E(t *testing.T) {
	run, err := e2eContextGate(os.Getenv(contextEnv), kubeCurrentContext)
	if err != nil {
		t.Fatal(err)
	}
	if !run {
		t.Skip("TestE2E reaches a real cluster, so it runs only when " + contextEnv + " names the current kubectl" +
			" context; use the e2e Task targets (task --list-all), which set it after confirming the context")
	}
	if suiteErr != nil {
		t.Fatal(suiteErr)
	}
	suiteConfig, reporterConfig := GinkgoConfiguration()
	// A mistyped label filter or an empty pack must be red, not a green run of nothing.
	suiteConfig.FailOnEmpty = true
	RegisterFailHandler(Fail)
	RunSpecs(t, "operator-testbench e2e suite", suiteConfig, reporterConfig)
}

// kubeContextName is the context the shell-outs get: none in-cluster.
func kubeContextName() string {
	if suite.Env.Context == inClusterContext {
		return ""
	}
	return suite.Env.Context
}

// connect builds the client every process uses. CRs stay unstructured, so no
// operator module is imported.
func connect() {
	GinkgoHelper()
	if k8sClient != nil {
		return
	}
	cfg, err := config.GetConfigWithContext(kubeContextName())
	Expect(err).NotTo(HaveOccurred(), "failed to load the kubeconfig")
	// A per-request timeout stops a dead API server from hanging the suite.
	cfg.Timeout = time.Minute
	k8sClient, err = client.New(cfg, client.Options{Scheme: clientgoscheme.Scheme})
	Expect(err).NotTo(HaveOccurred(), "failed to create the client")
}

func installer() *testbench.Installer {
	suiteConfig, _ := GinkgoConfiguration()
	return &testbench.Installer{
		Pack: suite.Pack, Context: kubeContextName(), Client: k8sClient,
		Command: commandContext, Log: GinkgoWriter, RetryDelay: testbench.RetryDelay,
		Procs: suiteConfig.ParallelTotal,
	}
}

// Process 1 installs the operator (or, with TESTBENCH_OPERATOR_INSTALL=skip,
// only waits for one); every process connects.
var _ = SynchronizedBeforeSuite(func(ctx SpecContext) []byte {
	connect()
	inst := installer()
	if suite.Env.Install == testbench.InstallSkip {
		By("waiting for the operator already installed in " + suite.Pack.Spec.Operator.Namespace)
		Expect(inst.WaitReady(ctx)).To(Succeed())
		return nil
	}
	By(fmt.Sprintf("installing %s %s into %s", suite.Pack.Name, suite.Pack.Version, suite.Pack.Spec.Operator.Namespace))
	Expect(inst.Install(ctx, suite.Env.Force)).To(Succeed())
	return nil
}, func(_ SpecContext, _ []byte) {
	connect()
}, NodeTimeout(teardownTimeout))

// Process 1 tears down after every other process has exited: the scenario
// namespaces first, while the operator still runs its finalizers, then the
// operator. Every step runs even after one fails.
var _ = SynchronizedAfterSuite(func() {}, func(ctx SpecContext) {
	Expect(testbench.RunEach(
		func() error {
			if suite.Env.Keep {
				By("keeping the scenario namespaces of pack " + suite.Pack.Name)
				return nil
			}
			By("deleting every scenario namespace of pack " + suite.Pack.Name)
			return testbench.DeleteLabelledNamespaces(ctx, k8sClient, suite.Pack.Name, teardownTimeout/2)
		},
		func() error {
			if suite.Env.Install == testbench.InstallSkip {
				return nil
			}
			By("uninstalling the operator")
			return installer().Uninstall(ctx)
		},
	)).To(Succeed(), "the suite's teardown left state behind")
}, NodeTimeout(teardownTimeout))
