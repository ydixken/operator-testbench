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
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// helmTimeout is what helm itself waits for the chart's workloads.
	helmTimeout = "5m"
	// readyTimeout bounds the wait for a controller pod to be Ready.
	readyTimeout = 5 * time.Minute
	// commandAttempts is how often a helm or kubectl shell-out is tried.
	commandAttempts = 3
	// RetryDelay is the production pause between two attempts.
	RetryDelay = 5 * time.Second
)

// ErrReleaseHeld is returned when another run holds the pack's Helm release.
var ErrReleaseHeld = errors.New("helm release exists")

// Installer puts the pack's operator on the cluster, once per run, and takes
// it away again. Helm and kubectl run as commands, the way a person would run
// them, so a failure is reproducible by hand.
type Installer struct {
	Pack    *Pack
	Context string
	Client  client.Client
	Command CommandFactory
	Log     io.Writer
	// RetryDelay is the pause between two attempts of a shell-out; tests set 0.
	RetryDelay time.Duration
	// ReadyTimeout bounds WaitReady; zero means readyTimeout.
	ReadyTimeout time.Duration
}

func (i *Installer) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(i.Log, format, args...)
}

// helmArgs prepends the kube context, so helm reaches the cluster the gate admitted.
func helmArgs(kubeContext string, args ...string) []string {
	if kubeContext == "" {
		return args
	}
	return append([]string{"--kube-context", kubeContext}, args...)
}

// run executes one command with bounded retries and logs its redacted output.
func (i *Installer) run(ctx context.Context, name string, args ...string) error {
	var last error
	for attempt := 1; attempt <= commandAttempts; attempt++ {
		out, err := i.Command(ctx, name, args...).CombinedOutput()
		if len(out) > 0 {
			i.logf("%s\n", redactLines(string(out)))
		}
		if err == nil {
			return nil
		}
		last = fmt.Errorf("%s %s: %w: %s", name, redactLine(strings.Join(args, " ")), err,
			redactLine(strings.TrimSpace(string(out))))
		if attempt < commandAttempts {
			i.logf("%s failed (attempt %d of %d), retrying\n", name, attempt, commandAttempts)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(i.RetryDelay):
			}
		}
	}
	return last
}

// ReleaseExists is the single-tenant lock: a release under the pack's name in
// the operator namespace means another run is active, or a crashed one left it.
func (i *Installer) ReleaseExists(ctx context.Context) (bool, error) {
	h := i.Pack.Spec.Install.Helm
	args := helmArgs(i.Context, "status", h.Release, "-n", i.Pack.Spec.Operator.Namespace)
	out, err := i.Command(ctx, "helm", args...).CombinedOutput()
	switch {
	case err == nil:
		return true, nil
	case strings.Contains(string(out), "not found"):
		return false, nil
	default:
		return false, fmt.Errorf("helm status %s: %w: %s", h.Release, err, redactLine(strings.TrimSpace(string(out))))
	}
}

// ApplyCRDs applies the pack's CRD manifests server-side, before the chart,
// because Helm never upgrades the CRDs a chart carries.
func (i *Installer) ApplyCRDs(ctx context.Context) error {
	for _, url := range i.Pack.CRDs {
		args := kubectlArgs(i.Context, "apply", "--server-side", "--force-conflicts",
			"--field-manager", FieldOwner, "-f", url)
		if err := i.run(ctx, "kubectl", args...); err != nil {
			return err
		}
	}
	return nil
}

// EnsureOperatorNamespace creates the operator namespace with the engine's
// label, so Uninstall can tell its own namespace from one that was there.
func (i *Installer) EnsureOperatorNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name:   i.Pack.Spec.Operator.Namespace,
		Labels: map[string]string{LabelManagedBy: FieldOwner, LabelPack: i.Pack.Name},
	}}
	if err := i.Client.Create(ctx, ns); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", ns.Name, err)
	}
	return nil
}

// HelmInstall installs the chart at its pinned version and waits for it.
func (i *Installer) HelmInstall(ctx context.Context) error {
	h := i.Pack.Spec.Install.Helm
	args := make([]string, 0, 14+2*len(i.Pack.HelmSet))
	args = append(args, "install", h.Release, h.Chart, "--repo", h.Repo, "--version", h.Version,
		"-n", i.Pack.Spec.Operator.Namespace, "--skip-crds", "--wait", "--timeout", helmTimeout)
	for _, k := range slices.Sorted(maps.Keys(i.Pack.HelmSet)) {
		args = append(args, "--set", k+"="+i.Pack.HelmSet[k])
	}
	return i.run(ctx, "helm", helmArgs(i.Context, args...)...)
}

// HelmUninstall removes the release; a release that is already gone is fine.
func (i *Installer) HelmUninstall(ctx context.Context) error {
	h := i.Pack.Spec.Install.Helm
	return i.run(ctx, "helm", helmArgs(i.Context, "uninstall", h.Release, "-n", i.Pack.Spec.Operator.Namespace,
		"--ignore-not-found", "--wait")...)
}

// WaitReady waits until a pod matching the pack's controller selector is Ready.
// It is the one check shared by the install path and TESTBENCH_OPERATOR_INSTALL=skip.
func (i *Installer) WaitReady(ctx context.Context) error {
	sel, err := labels.Parse(i.Pack.Spec.Operator.Selector)
	if err != nil {
		return fmt.Errorf("operator.selector: %w", err)
	}
	timeout := i.ReadyTimeout
	if timeout == 0 {
		timeout = readyTimeout
	}
	ns := i.Pack.Spec.Operator.Namespace
	return poll(ctx, timeout, func(ctx context.Context) error {
		var pods corev1.PodList
		opts := []client.ListOption{client.InNamespace(ns), client.MatchingLabelsSelector{Selector: sel}}
		if err := i.Client.List(ctx, &pods, opts...); err != nil {
			return err
		}
		for _, p := range pods.Items {
			for _, c := range p.Status.Conditions {
				if c.Type == corev1.PodReady && c.Status == corev1.ConditionTrue {
					return nil
				}
			}
		}
		return fmt.Errorf("no Ready pod matches %s in namespace %s (%d pods); with %s=%s the operator must already run there",
			i.Pack.Spec.Operator.Selector, ns, len(pods.Items), EnvInstall, InstallSkip)
	})
}

// Install is the whole install path: the lock, the CRDs, the namespace, the
// chart, the readiness wait. force takes over a release a crashed run left.
func (i *Installer) Install(ctx context.Context, force bool) error {
	held, err := i.ReleaseExists(ctx)
	if err != nil {
		return err
	}
	if held {
		h := i.Pack.Spec.Install.Helm
		if !force {
			return fmt.Errorf("%w: %s in %s. Another run is active, or a crashed run left it behind; the suite is"+
				" single-tenant per cluster. Wait for the other run, or rerun with %s=true to take the release over",
				ErrReleaseHeld, h.Release, i.Pack.Spec.Operator.Namespace, EnvForce)
		}
		i.logf("%s=true: taking over release %s\n", EnvForce, h.Release)
		if err := i.HelmUninstall(ctx); err != nil {
			return err
		}
	}
	if err := i.ApplyCRDs(ctx); err != nil {
		return err
	}
	if err := i.EnsureOperatorNamespace(ctx); err != nil {
		return err
	}
	if err := i.HelmInstall(ctx); err != nil {
		return err
	}
	return i.WaitReady(ctx)
}

// Uninstall removes the release and then the operator namespace, but only a
// namespace the engine created; the CRDs stay, because deleting a CRD deletes
// every object of that kind cluster-wide.
func (i *Installer) Uninstall(ctx context.Context) error {
	if err := i.HelmUninstall(ctx); err != nil {
		return err
	}
	var ns corev1.Namespace
	err := i.Client.Get(ctx, client.ObjectKey{Name: i.Pack.Spec.Operator.Namespace}, &ns)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return fmt.Errorf("get namespace %s: %w", i.Pack.Spec.Operator.Namespace, err)
	case ns.Labels[LabelManagedBy] != FieldOwner:
		i.logf("namespace %s was not created by this suite, leaving it\n", ns.Name)
		return nil
	}
	if err := i.Client.Delete(ctx, &ns); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %s: %w", ns.Name, err)
	}
	return poll(ctx, 2*time.Minute, func(ctx context.Context) error {
		err := i.Client.Get(ctx, client.ObjectKey{Name: ns.Name}, &corev1.Namespace{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("namespace %s still terminating", ns.Name)
	})
}
