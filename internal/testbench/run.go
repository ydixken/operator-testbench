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
	"io"
	"io/fs"
	"maps"
	"os/exec"
	"path"
	"slices"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Labels the engine puts on the namespaces it creates, so a teardown can find
// them and a human can tell them from everything else.
const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelPack      = "testbench.dev/pack"
	LabelScenario  = "testbench.dev/scenario"
)

// cleanupTimeout bounds each wait during a Run's cleanup.
const cleanupTimeout = 3 * time.Minute

// CommandFactory builds the kubectl and helm commands, so a test can swap in
// a fake. exec.CommandContext is the production value.
type CommandFactory func(ctx context.Context, name string, args ...string) *exec.Cmd

// GoStepFunc is a registered Go step. Whatever it returns merges into the
// scenario's Vars for the steps after it.
type GoStepFunc func(ctx context.Context, r *Run, args map[string]string) (map[string]string, error)

// Run is one scenario on one Ginkgo process: its namespace, its vars and what
// it applied, which its Cleanup removes again.
type Run struct {
	Client client.Client
	// Context is the kubeconfig context for kubectl; empty means none (in-cluster).
	Context   string
	Namespace string
	Pack      *Pack
	Scenario  *Scenario
	Vars      map[string]string
	Command   CommandFactory
	Log       io.Writer
	// Keep leaves the namespace and its objects in place at the end.
	Keep bool

	goSteps map[string]GoStepFunc
	applied []appliedObject
}

type appliedObject struct {
	gvk schema.GroupVersionKind
	key client.ObjectKey
}

// NewRun prepares the scenario's run on Ginkgo process proc.
func NewRun(c client.Client, kubeContext string, pack *Pack, sc *Scenario, proc int, env Env,
	steps map[string]GoStepFunc, cmd CommandFactory, log io.Writer) *Run {
	return &Run{
		Client:    c,
		Context:   kubeContext,
		Namespace: NamespaceFor(pack.Name, sc.Name, proc),
		Pack:      pack,
		Scenario:  sc,
		Vars:      map[string]string{},
		Command:   cmd,
		Log:       log,
		Keep:      env.Keep,
		goSteps:   steps,
	}
}

// EnsureNamespace creates the scenario namespace, or adopts one a kept run left.
func (r *Run) EnsureNamespace(ctx context.Context) error {
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, Labels: map[string]string{
		LabelManagedBy: FieldOwner, LabelPack: r.Pack.Name, LabelScenario: r.Scenario.Name,
	}}}
	err := r.Client.Create(ctx, ns)
	if apierrors.IsAlreadyExists(err) {
		r.logf("namespace %s exists, reusing it\n", r.Namespace)
		return nil
	}
	return err
}

// Step runs one step and returns its failure, if any.
func (r *Run) Step(ctx context.Context, st *Step) error {
	kind, err := st.Kind()
	if err != nil {
		return err
	}
	switch kind {
	case kindApply:
		return r.apply(ctx, st.Apply)
	case kindAssert:
		return r.assert(ctx, st)
	case kindExec:
		return r.exec(ctx, st)
	case kindDelete:
		return r.delete(ctx, st)
	default:
		return r.goStep(ctx, st.Go)
	}
}

// logf writes to the run's log; a failed write is not a failed step.
func (r *Run) logf(format string, args ...any) {
	_, _ = fmt.Fprintf(r.Log, format, args...)
}

func (r *Run) render(s string) (string, error) {
	return Render(s, TemplateData{Namespace: r.Namespace, Version: r.Pack.Version, Vars: r.Vars})
}

// poll retries attempt every Poll until it returns nil or the timeout passes,
// and then reports the last reason it had not succeeded.
func poll(ctx context.Context, timeout time.Duration, attempt func(ctx context.Context) error) error {
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var last error
	for {
		if last = attempt(deadline); last == nil {
			return nil
		}
		select {
		case <-deadline.Done():
			if ctx.Err() != nil {
				return fmt.Errorf("interrupted: %w", last)
			}
			return fmt.Errorf("not satisfied after %s: %w", timeout, last)
		case <-time.After(Poll):
		}
	}
}

// decodeManifest splits a multi-document manifest into objects, each with an
// apiVersion, a kind and a name.
func decodeManifest(data []byte) ([]*unstructured.Unstructured, error) {
	dec := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	var objs []*unstructured.Unstructured
	for i := 0; ; i++ {
		u := &unstructured.Unstructured{}
		err := dec.Decode(u)
		if errors.Is(err, io.EOF) {
			return objs, nil
		}
		if err != nil {
			return nil, fmt.Errorf("document %d: %w", i+1, err)
		}
		if len(u.Object) == 0 {
			continue // an empty document, for example a trailing ---
		}
		if u.GetAPIVersion() == "" || u.GetKind() == "" || u.GetName() == "" {
			return nil, fmt.Errorf("document %d needs apiVersion, kind and metadata.name", i+1)
		}
		objs = append(objs, u)
	}
}

// namespaced reports whether objects of u's kind live in a namespace.
func (r *Run) namespaced(u *unstructured.Unstructured) (bool, error) {
	ok, err := r.Client.IsObjectNamespaced(u)
	if err != nil {
		return false, fmt.Errorf("%s %s: %w", u.GetKind(), u.GetName(), err)
	}
	return ok, nil
}

func (r *Run) apply(ctx context.Context, a *ApplyStep) error {
	data, err := fs.ReadFile(r.Pack.FS, path.Join(r.Pack.Name, a.File))
	if err != nil {
		return err
	}
	rendered, err := r.render(string(data))
	if err != nil {
		return fmt.Errorf("%s: %w", a.File, err)
	}
	objs, err := decodeManifest([]byte(rendered))
	if err != nil {
		return fmt.Errorf("%s: %w", a.File, err)
	}
	for _, u := range objs {
		namespaced, err := r.namespaced(u)
		if err != nil {
			return err
		}
		switch {
		case namespaced && u.GetNamespace() == "":
			u.SetNamespace(r.Namespace)
		case namespaced && u.GetNamespace() != r.Namespace:
			return fmt.Errorf("%s %s names namespace %q; manifest objects may only live in the scenario namespace",
				u.GetKind(), u.GetName(), u.GetNamespace())
		}
		if err := r.Client.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
			client.FieldOwner(FieldOwner), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply %s %s: %w", u.GetKind(), u.GetName(), err)
		}
		r.logf("applied %s %s\n", u.GetKind(), u.GetName())
		r.record(u)
	}
	return nil
}

// record remembers an applied object once, so a re-apply of the same object
// does not delete it twice at cleanup.
func (r *Run) record(u *unstructured.Unstructured) {
	o := appliedObject{gvk: u.GroupVersionKind(), key: client.ObjectKeyFromObject(u)}
	if !slices.Contains(r.applied, o) {
		r.applied = append(r.applied, o)
	}
}

// objectFor turns a reference into an empty object with its GVK, name and,
// for a namespaced kind, the scenario namespace.
func (r *Run) objectFor(ref *ObjectRef) (*unstructured.Unstructured, error) {
	name, err := r.render(ref.Name)
	if err != nil {
		return nil, fmt.Errorf("name %q: %w", ref.Name, err)
	}
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind))
	u.SetName(name)
	namespaced, err := r.namespaced(u)
	if err != nil {
		return nil, err
	}
	if namespaced {
		u.SetNamespace(r.Namespace)
	}
	return u, nil
}

// list fetches the objects a reference selects in the scenario namespace.
func (r *Run) list(ctx context.Context, ref *ListRef) (*unstructured.UnstructuredList, error) {
	selector, err := r.render(ref.Selector)
	if err != nil {
		return nil, fmt.Errorf("selector %q: %w", ref.Selector, err)
	}
	sel, err := labels.Parse(selector)
	if err != nil {
		return nil, fmt.Errorf("selector %q: %w", selector, err)
	}
	l := &unstructured.UnstructuredList{}
	gvk := schema.FromAPIVersionAndKind(ref.APIVersion, ref.Kind)
	l.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
	opts := []client.ListOption{client.InNamespace(r.Namespace), client.MatchingLabelsSelector{Selector: sel}}
	if err := r.Client.List(ctx, l, opts...); err != nil {
		return nil, fmt.Errorf("list %s with %s: %w", ref.Kind, selector, err)
	}
	return l, nil
}

func (r *Run) assert(ctx context.Context, st *Step) error {
	a := st.Assert
	if a.Object != nil {
		u, err := r.objectFor(a.Object)
		if err != nil {
			return err
		}
		return poll(ctx, StepTimeout(st), func(ctx context.Context) error {
			live := u.DeepCopy()
			err := r.Client.Get(ctx, client.ObjectKeyFromObject(u), live)
			switch {
			case a.Absent && apierrors.IsNotFound(err):
				return nil
			case a.Absent && err == nil:
				return fmt.Errorf("%s %s still exists", u.GetKind(), u.GetName())
			case err != nil:
				return fmt.Errorf("get %s %s: %w", u.GetKind(), u.GetName(), err)
			}
			if a.Subset() != nil {
				if err := checkSubset(a.Subset(), live); err != nil {
					return err
				}
			}
			for _, prg := range a.Programs() {
				if err := prg.Check(ctx, live.Object, nil); err != nil {
					return fmt.Errorf("%w\nobject:\n%s", err, indent(ForReport(live)))
				}
			}
			return nil
		})
	}
	return poll(ctx, StepTimeout(st), func(ctx context.Context) error {
		l, err := r.list(ctx, a.List)
		if err != nil {
			return err
		}
		if a.Count != nil && len(l.Items) != *a.Count {
			return fmt.Errorf("%d %s match %s, want %d", len(l.Items), a.List.Kind, a.List.Selector, *a.Count)
		}
		items := make([]any, 0, len(l.Items))
		for i := range l.Items {
			items = append(items, l.Items[i].Object)
		}
		for _, prg := range a.Programs() {
			if err := prg.Check(ctx, nil, items); err != nil {
				return fmt.Errorf("%w\nitems: %d %s matching %s", err, len(items), a.List.Kind, a.List.Selector)
			}
		}
		return nil
	})
}

// kubectlArgs prepends the context, so every shell-out reaches the cluster the
// gate admitted.
func kubectlArgs(kubeContext string, args ...string) []string {
	if kubeContext == "" {
		return args
	}
	return append([]string{"--context", kubeContext}, args...)
}

// kubectlExecArgs is the argv before the command itself.
func kubectlExecArgs(kubeContext, namespace, pod, container string) []string {
	return kubectlArgs(kubeContext, "exec", "-n", namespace, pod, "-c", container, "--")
}

// pickPod is the first Running pod the selector matches, by name.
func (r *Run) pickPod(ctx context.Context, selector string) (string, error) {
	l, err := r.list(ctx, &ListRef{APIVersion: "v1", Kind: "Pod", Selector: selector})
	if err != nil {
		return "", err
	}
	var names []string
	for i := range l.Items {
		phase, _, _ := unstructured.NestedString(l.Items[i].Object, "status", "phase")
		if phase == string(corev1.PodRunning) {
			names = append(names, l.Items[i].GetName())
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no Running pod matches %s", selector)
	}
	slices.Sort(names)
	return names[0], nil
}

// ExecOutput runs one command in a container once and returns its stdout,
// for Go steps that need to read something back.
func (r *Run) ExecOutput(ctx context.Context, pod, container string, command ...string) ([]byte, error) {
	attempt, cancel := context.WithTimeout(ctx, ExecAttemptTimeout)
	defer cancel()
	argv := append(kubectlExecArgs(r.Context, r.Namespace, pod, container), command...)
	cmd := r.Command(attempt, "kubectl", argv...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	r.logf("exec %s: %s\n", pod, redactLine(strings.Join(command, " ")))
	if err != nil {
		return out, fmt.Errorf("exec in %s/%s: %w: %s", pod, container, err, redactLine(strings.TrimSpace(stderr.String())))
	}
	return out, nil
}

func (r *Run) exec(ctx context.Context, st *Step) error {
	e := st.Exec
	command := make([]string, len(e.Command))
	for i, arg := range e.Command {
		s, err := r.render(arg)
		if err != nil {
			return fmt.Errorf("command[%d]: %w", i, err)
		}
		command[i] = s
	}
	var want []string
	if e.Expect != nil {
		for i, s := range e.Expect.Contains {
			rendered, err := r.render(s)
			if err != nil {
				return fmt.Errorf("expect.contains[%d]: %w", i, err)
			}
			want = append(want, rendered)
		}
	}
	pod, err := r.render(e.Pod)
	if err != nil {
		return fmt.Errorf("pod: %w", err)
	}
	return poll(ctx, StepTimeout(st), func(ctx context.Context) error {
		target := pod
		if target == "" {
			if target, err = r.pickPod(ctx, e.Selector); err != nil {
				return err
			}
		}
		out, err := r.ExecOutput(ctx, target, e.Container, command...)
		if err != nil {
			return err
		}
		text := string(out)
		var missing []string
		for _, s := range want {
			if !strings.Contains(text, s) {
				missing = append(missing, s)
			}
		}
		if len(missing) > 0 {
			return fmt.Errorf("stdout of %s lacks %q; got:\n%s", target, missing, indent(redactLines(text)))
		}
		return nil
	})
}

func redactLines(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = redactLine(line)
	}
	return strings.Join(lines, "\n")
}

// deleteObject deletes u in the foreground and waits until it is gone or has
// been recreated under a new UID, which is how a StatefulSet replaces a pod
// of the same name. Deleting what is already gone is fine.
func (r *Run) deleteObject(ctx context.Context, u *unstructured.Unstructured, timeout time.Duration) error {
	live := u.DeepCopy()
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(u), live); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("get %s %s: %w", u.GetKind(), u.GetName(), err)
	}
	uid := live.GetUID()
	err := r.Client.Delete(ctx, live, client.PropagationPolicy(metav1.DeletePropagationForeground))
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete %s %s: %w", u.GetKind(), u.GetName(), err)
	}
	r.logf("deleted %s %s\n", u.GetKind(), u.GetName())
	return poll(ctx, timeout, func(ctx context.Context) error {
		cur := u.DeepCopy()
		err := r.Client.Get(ctx, client.ObjectKeyFromObject(u), cur)
		switch {
		case apierrors.IsNotFound(err), err == nil && cur.GetUID() != uid:
			return nil
		case err != nil:
			return fmt.Errorf("get %s %s: %w", u.GetKind(), u.GetName(), err)
		}
		return fmt.Errorf("%s %s is still being deleted", u.GetKind(), u.GetName())
	})
}

func (r *Run) delete(ctx context.Context, st *Step) error {
	d := st.Delete
	if d.Object != nil {
		u, err := r.objectFor(d.Object)
		if err != nil {
			return err
		}
		return r.deleteObject(ctx, u, StepTimeout(st))
	}
	l, err := r.list(ctx, d.List)
	if err != nil {
		return err
	}
	if len(l.Items) == 0 {
		return fmt.Errorf("nothing matches %s %s; a delete that finds nothing is a mistake in the scenario",
			d.List.Kind, d.List.Selector)
	}
	uids := make(map[string]types.UID, len(l.Items))
	for i := range l.Items {
		item := &l.Items[i]
		uids[item.GetName()] = item.GetUID()
		err := r.Client.Delete(ctx, item, client.PropagationPolicy(metav1.DeletePropagationForeground))
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete %s %s: %w", item.GetKind(), item.GetName(), err)
		}
	}
	r.logf("deleted %d %s matching %s\n", len(l.Items), d.List.Kind, d.List.Selector)
	return poll(ctx, StepTimeout(st), func(ctx context.Context) error {
		cur, err := r.list(ctx, d.List)
		if err != nil {
			return err
		}
		for i := range cur.Items {
			if uid, deleted := uids[cur.Items[i].GetName()]; deleted && uid == cur.Items[i].GetUID() {
				return fmt.Errorf("%s %s is still being deleted", cur.Items[i].GetKind(), cur.Items[i].GetName())
			}
		}
		return nil
	})
}

func (r *Run) goStep(ctx context.Context, g *GoStep) error {
	fn, ok := r.goSteps[g.Name]
	if !ok {
		return fmt.Errorf("go step %q is not registered", g.Name)
	}
	args := make(map[string]string, len(g.Args))
	for k, v := range g.Args {
		s, err := r.render(v)
		if err != nil {
			return fmt.Errorf("args[%s]: %w", k, err)
		}
		args[k] = s
	}
	out, err := fn(ctx, r, args)
	if err != nil {
		return fmt.Errorf("go step %s: %w", g.Name, err)
	}
	maps.Copy(r.Vars, out)
	return nil
}

// Cleanup removes what the run applied, newest first, and then its namespace,
// unless the run keeps its fixtures. Every step runs even after one fails.
func (r *Run) Cleanup(ctx context.Context) error {
	if r.Keep {
		r.logf("keeping namespace %s and its %d applied objects\n", r.Namespace, len(r.applied))
		return nil
	}
	steps := make([]func() error, 0, len(r.applied)+1)
	for _, o := range slices.Backward(r.applied) {
		steps = append(steps, func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(o.gvk)
			u.SetName(o.key.Name)
			u.SetNamespace(o.key.Namespace)
			return r.deleteObject(ctx, u, cleanupTimeout)
		})
	}
	steps = append(steps, func() error {
		ns := &unstructured.Unstructured{}
		ns.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Namespace"))
		ns.SetName(r.Namespace)
		return r.deleteObject(ctx, ns, cleanupTimeout)
	})
	return RunEach(steps...)
}

// RunEach runs every step and joins the failures, so one failed teardown step
// never skips the ones after it.
func RunEach(steps ...func() error) error {
	var errs []error
	for _, step := range steps {
		if err := step(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// DeleteLabelledNamespaces removes every namespace a pack's scenarios left,
// including those of other process counts, and waits until they are gone.
func DeleteLabelledNamespaces(ctx context.Context, c client.Client, pack string, timeout time.Duration) error {
	var list corev1.NamespaceList
	if err := c.List(ctx, &list, client.MatchingLabels{LabelPack: pack}); err != nil {
		return fmt.Errorf("list namespaces of pack %s: %w", pack, err)
	}
	for i := range list.Items {
		if err := c.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete namespace %s: %w", list.Items[i].Name, err)
		}
	}
	return poll(ctx, timeout, func(ctx context.Context) error {
		var left corev1.NamespaceList
		if err := c.List(ctx, &left, client.MatchingLabels{LabelPack: pack}); err != nil {
			return err
		}
		if len(left.Items) > 0 {
			return fmt.Errorf("%d namespaces of pack %s still terminating", len(left.Items), pack)
		}
		return nil
	})
}
