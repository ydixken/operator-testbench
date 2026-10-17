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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"

	"k8s.io/apimachinery/pkg/api/validation"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utiljson "k8s.io/apimachinery/pkg/util/json"
)

const (
	// APIVersion is the only schema version the loader accepts.
	APIVersion = "testbench.dev/v1alpha1"
	// KindOperatorPack is operators/<pack>/operator.yaml.
	KindOperatorPack = "OperatorPack"
	// KindScenario is operators/<pack>/scenarios/<name>.yaml.
	KindScenario = "Scenario"

	// FieldOwner identifies the suite's server-side applies.
	FieldOwner = "operator-testbench"
	// DefaultStepTimeout bounds a step that names none.
	DefaultStepTimeout = 2 * time.Minute
	// Poll is the interval between two attempts of an assert or exec step.
	Poll = 2 * time.Second
	// ExecAttemptTimeout bounds one kubectl exec attempt inside an exec step.
	ExecAttemptTimeout = 30 * time.Second
	// NodeTimeoutGrace is added to a step's timeout for the Ginkgo NodeTimeout,
	// so the Eventually deadline reports first with its own message.
	NodeTimeoutGrace = 30 * time.Second

	// namespacePrefix starts every scenario namespace.
	namespacePrefix = "testbench"
	// maxPackName and maxScenarioName keep "testbench-<pack>-<scenario>-999"
	// inside the 63 characters of a DNS label.
	maxPackName     = 16
	maxScenarioName = 30
)

// OperatorPack is operators/<pack>/operator.yaml: how to install one pinned
// version of the operator under test.
type OperatorPack struct {
	APIVersion string       `json:"apiVersion"`
	Kind       string       `json:"kind"`
	Operator   OperatorSpec `json:"operator"`
	Install    InstallSpec  `json:"install"`
}

// OperatorSpec names the operator version, where it runs and how to find it.
type OperatorSpec struct {
	// Version is the default operator version; TESTBENCH_OPERATOR_VERSION
	// overrides it, and the pack's templates read it as {{ .Version }}.
	Version string `json:"version"`
	// Namespace is where the engine installs the operator. The engine
	// creates it, so it can tell its own namespace from one that was there.
	Namespace string `json:"namespace"`
	// Selector matches the controller pods in Namespace: the readiness wait
	// and the failure log tail both use it.
	Selector string `json:"selector"`
}

// InstallSpec is the install recipe: CRDs first, then the Helm chart.
type InstallSpec struct {
	// CRDs are manifest URLs applied server-side before the chart, because
	// Helm never upgrades CRDs. Templated with {{ .Version }} only.
	CRDs []string `json:"crds,omitempty"`
	// Helm is the chart to install; the only install method so far.
	Helm *HelmSpec `json:"helm,omitempty"`
}

// HelmSpec is a chart in a Helm repository at a pinned version.
type HelmSpec struct {
	Repo    string `json:"repo"`
	Chart   string `json:"chart"`
	Version string `json:"version"`
	Release string `json:"release"`
	// Set holds --set key=value pairs; values are templated with {{ .Version }}.
	Set map[string]string `json:"set,omitempty"`
}

// Scenario is one scenarios/<name>.yaml: an ordered list of steps that run in
// their own namespace.
type Scenario struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Labels     []string `json:"labels,omitempty"`
	Priority   int      `json:"priority,omitempty"`
	Steps      []Step   `json:"steps"`

	// Name is the file's base name and Path its location; the loader sets both.
	Name string `json:"-"`
	Path string `json:"-"`
}

// Step is one action with its own timeout. Exactly one of the kind fields is set.
type Step struct {
	Name    string           `json:"name"`
	Timeout *metav1.Duration `json:"timeout,omitempty"`

	Apply  *ApplyStep  `json:"apply,omitempty"`
	Assert *AssertStep `json:"assert,omitempty"`
	Exec   *ExecStep   `json:"exec,omitempty"`
	Delete *DeleteStep `json:"delete,omitempty"`
	Go     *GoStep     `json:"go,omitempty"`
}

// ApplyStep server-side applies every object in a manifest file of the pack.
type ApplyStep struct {
	// File is relative to the pack directory, for example manifests/cluster.yaml.
	File string `json:"file"`
}

// ObjectRef names one object. Name may be a template.
type ObjectRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
}

// ListRef selects objects of one kind in the scenario namespace by label.
type ListRef struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Selector   string `json:"selector"`
}

// AssertStep polls until an object or list looks as expected.
type AssertStep struct {
	Object *ObjectRef `json:"object,omitempty"`
	List   *ListRef   `json:"list,omitempty"`
	// Match is a subset the live object must contain (object only).
	Match json.RawMessage `json:"match,omitempty"`
	// CEL holds bool expressions over self (object) or items (list).
	CEL []string `json:"cel,omitempty"`
	// Count is the exact list length (list only); 0 asserts nothing is left.
	Count *int `json:"count,omitempty"`
	// Absent passes once the object is NotFound (object only).
	Absent bool `json:"absent,omitempty"`

	match    map[string]any
	programs []*Program
}

// ExecStep runs a command in a container and checks its output.
type ExecStep struct {
	// Pod names the pod (templated); Selector picks the first Running pod by
	// sorted name instead. Exactly one is set.
	Pod       string   `json:"pod,omitempty"`
	Selector  string   `json:"selector,omitempty"`
	Container string   `json:"container"`
	Command   []string `json:"command"`
	Expect    *Expect  `json:"expect,omitempty"`
}

// Expect is what an exec's stdout must contain. Every entry must appear.
type Expect struct {
	Contains []string `json:"contains,omitempty"`
}

// DeleteStep deletes one object or every object a selector matches, and waits.
type DeleteStep struct {
	Object *ObjectRef `json:"object,omitempty"`
	List   *ListRef   `json:"list,omitempty"`
}

// GoStep calls a registered Go function by name; its result merges into .Vars.
type GoStep struct {
	Name string            `json:"name"`
	Args map[string]string `json:"args,omitempty"`
}

// Subset is the decoded match of an assert step, nil when none was given.
func (a *AssertStep) Subset() map[string]any { return a.match }

// Programs are the compiled CEL expressions of an assert step.
func (a *AssertStep) Programs() []*Program { return a.programs }

// Kind reports which step kind is set, or an error when zero or several are.
func (st *Step) Kind() (string, error) {
	var kinds []string
	for name, set := range map[string]bool{
		"apply": st.Apply != nil, "assert": st.Assert != nil, "exec": st.Exec != nil,
		"delete": st.Delete != nil, "go": st.Go != nil,
	} {
		if set {
			kinds = append(kinds, name)
		}
	}
	switch len(kinds) {
	case 1:
		return kinds[0], nil
	case 0:
		return "", errors.New("step has no kind; set one of apply, assert, exec, delete, go")
	default:
		return "", fmt.Errorf("step has %d kinds; set exactly one of apply, assert, exec, delete, go", len(kinds))
	}
}

// StepTimeout is the step's own timeout or the default.
func StepTimeout(st *Step) time.Duration {
	if st.Timeout != nil {
		return st.Timeout.Duration
	}
	return DefaultStepTimeout
}

// NamespaceFor is the scenario's namespace on a Ginkgo process: process 1
// keeps the plain name so a one-process run and its kept namespaces look the
// same every time, process N adds -N.
func NamespaceFor(pack, scenario string, proc int) string {
	ns := namespacePrefix + "-" + pack + "-" + scenario
	if proc > 1 {
		ns += "-" + strconv.Itoa(proc)
	}
	return ns
}

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validateName(what, name string, maxLen int) error {
	if !dnsLabel.MatchString(name) {
		return fmt.Errorf("%s %q must be a DNS label: lowercase letters, digits and dashes", what, name)
	}
	if len(name) > maxLen {
		return fmt.Errorf("%s %q is longer than %d characters", what, name, maxLen)
	}
	return nil
}

// validateLabel applies Ginkgo's label rules, so a bad label fails at load with
// the file named rather than inside RunSpecs.
func validateLabel(label string) error {
	trimmed := strings.TrimSpace(label)
	switch {
	case trimmed == "":
		return errors.New("label is empty")
	case strings.ContainsAny(trimmed, "&|!,()/"):
		return fmt.Errorf("label %q contains one of &|!,()/", label)
	case strings.HasPrefix(trimmed, ":"):
		return fmt.Errorf("label %q starts with ':'", label)
	}
	if i := strings.Index(trimmed, ":"); i >= 0 && strings.TrimSpace(trimmed[i+1:]) == "" {
		return fmt.Errorf("label %q has nothing after ':'", label)
	}
	return nil
}

func isTemplated(s string) bool { return strings.Contains(s, "{{") }

// checkTemplate parses s as a text/template so a typo fails at load.
func checkTemplate(s string) error {
	_, err := template.New("").Option("missingkey=error").Parse(s)
	return err
}

// checkSelector accepts a static label selector or a template.
func checkSelector(s string) error {
	if s == "" {
		return errors.New("selector is empty")
	}
	if isTemplated(s) {
		return checkTemplate(s)
	}
	if _, err := labels.Parse(s); err != nil {
		return fmt.Errorf("selector %q: %w", s, err)
	}
	return nil
}

func checkObjectRef(ref *ObjectRef) error {
	if _, err := schema.ParseGroupVersion(ref.APIVersion); err != nil {
		return fmt.Errorf("apiVersion %q: %w", ref.APIVersion, err)
	}
	if ref.Kind == "" {
		return errors.New("kind is empty")
	}
	if ref.Name == "" {
		return errors.New("name is empty")
	}
	if isTemplated(ref.Name) {
		return checkTemplate(ref.Name)
	}
	return nil
}

func checkListRef(ref *ListRef) error {
	if _, err := schema.ParseGroupVersion(ref.APIVersion); err != nil {
		return fmt.Errorf("apiVersion %q: %w", ref.APIVersion, err)
	}
	if ref.Kind == "" {
		return errors.New("kind is empty")
	}
	return checkSelector(ref.Selector)
}

// decodeMatch decodes the subset through apimachinery's JSON path, so integers
// become int64 like the live object's, and refuses the values DeepDerivative
// would treat as wildcards: an empty string, list or map, and null.
func decodeMatch(raw json.RawMessage) (map[string]any, error) {
	var v any
	if err := utiljson.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("match must be an object")
	}
	if err := rejectWildcards("match", m); err != nil {
		return nil, err
	}
	return m, nil
}

func rejectWildcards(path string, v any) error {
	switch x := v.(type) {
	case nil:
		return fmt.Errorf("%s is null, which matches anything", path)
	case string:
		if x == "" {
			return fmt.Errorf("%s is an empty string, which matches anything", path)
		}
	case map[string]any:
		if len(x) == 0 {
			return fmt.Errorf("%s is an empty map, which matches anything", path)
		}
		for k, val := range x {
			if err := rejectWildcards(path+"."+k, val); err != nil {
				return err
			}
		}
	case []any:
		if len(x) == 0 {
			return fmt.Errorf("%s is an empty list, which matches anything", path)
		}
		for i, val := range x {
			if err := rejectWildcards(path+"["+strconv.Itoa(i)+"]", val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *AssertStep) validate(env *CELEnv) error {
	switch {
	case a.Object == nil && a.List == nil:
		return errors.New("assert needs object or list")
	case a.Object != nil && a.List != nil:
		return errors.New("assert has both object and list; set one")
	}
	if a.Object != nil {
		if err := checkObjectRef(a.Object); err != nil {
			return fmt.Errorf("object: %w", err)
		}
		if a.Count != nil {
			return errors.New("count applies to a list, not an object")
		}
		if a.Absent && (len(a.Match) > 0 || len(a.CEL) > 0) {
			return errors.New("absent excludes match and cel")
		}
	} else {
		if err := checkListRef(a.List); err != nil {
			return fmt.Errorf("list: %w", err)
		}
		if len(a.Match) > 0 {
			return errors.New("match applies to an object; use cel over items for a list")
		}
		if a.Absent {
			return errors.New("absent applies to an object; use count: 0 for a list")
		}
		if a.Count == nil && len(a.CEL) == 0 {
			return errors.New("a list assert needs count or cel")
		}
		if a.Count != nil && *a.Count < 0 {
			return fmt.Errorf("count %d is negative", *a.Count)
		}
	}
	if len(a.Match) > 0 {
		m, err := decodeMatch(a.Match)
		if err != nil {
			return err
		}
		a.match = m
	}
	a.programs = a.programs[:0]
	for _, expr := range a.CEL {
		prg, err := env.CompileBool(expr)
		if err != nil {
			return err
		}
		a.programs = append(a.programs, prg)
	}
	return nil
}

func (e *ExecStep) validate() error {
	switch {
	case e.Pod == "" && e.Selector == "":
		return errors.New("exec needs pod or selector")
	case e.Pod != "" && e.Selector != "":
		return errors.New("exec has both pod and selector; set one")
	case e.Pod != "" && isTemplated(e.Pod):
		if err := checkTemplate(e.Pod); err != nil {
			return fmt.Errorf("pod: %w", err)
		}
	case e.Selector != "":
		if err := checkSelector(e.Selector); err != nil {
			return fmt.Errorf("selector: %w", err)
		}
	}
	if e.Container == "" {
		return errors.New("exec needs container")
	}
	if len(e.Command) == 0 {
		return errors.New("exec needs command")
	}
	for i, arg := range e.Command {
		if err := checkTemplate(arg); err != nil {
			return fmt.Errorf("command[%d]: %w", i, err)
		}
	}
	if e.Expect != nil {
		for i, s := range e.Expect.Contains {
			if s == "" {
				return fmt.Errorf("expect.contains[%d] is empty", i)
			}
			if err := checkTemplate(s); err != nil {
				return fmt.Errorf("expect.contains[%d]: %w", i, err)
			}
		}
	}
	return nil
}

func (d *DeleteStep) validate() error {
	switch {
	case d.Object == nil && d.List == nil:
		return errors.New("delete needs object or list")
	case d.Object != nil && d.List != nil:
		return errors.New("delete has both object and list; set one")
	case d.Object != nil:
		if err := checkObjectRef(d.Object); err != nil {
			return fmt.Errorf("object: %w", err)
		}
	default:
		if err := checkListRef(d.List); err != nil {
			return fmt.Errorf("list: %w", err)
		}
	}
	return nil
}

func (g *GoStep) validate(known map[string]bool) error {
	if !known[g.Name] {
		return fmt.Errorf("go step %q is not registered", g.Name)
	}
	for k, v := range g.Args {
		if k == "" {
			return errors.New("go args has an empty key")
		}
		if err := checkTemplate(v); err != nil {
			return fmt.Errorf("args[%s]: %w", k, err)
		}
	}
	return nil
}

func (st *Step) validate(known map[string]bool, env *CELEnv) error {
	if st.Name == "" {
		return errors.New("step has no name")
	}
	if st.Timeout != nil && st.Timeout.Duration <= 0 {
		return fmt.Errorf("timeout %s is not positive", st.Timeout.Duration)
	}
	kind, err := st.Kind()
	if err != nil {
		return err
	}
	switch kind {
	case "apply":
		if st.Apply.File == "" || !fs.ValidPath(st.Apply.File) {
			return fmt.Errorf("apply.file %q must be a relative path inside the pack", st.Apply.File)
		}
		return nil
	case "assert":
		return st.Assert.validate(env)
	case "exec":
		return st.Exec.validate()
	case "delete":
		return st.Delete.validate()
	default:
		return st.Go.validate(known)
	}
}

// Validate checks a parsed scenario against the rules the engine relies on and
// compiles its CEL. known holds the registered Go step names. Every problem is
// reported, each prefixed with the file and the step.
func (s *Scenario) Validate(pack string, known map[string]bool, env *CELEnv) error {
	var errs []error
	fail := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf(s.Path+": "+format, args...))
	}
	if s.APIVersion != APIVersion {
		fail("apiVersion %q, want %s", s.APIVersion, APIVersion)
	}
	if s.Kind != KindScenario {
		fail("kind %q, want %s", s.Kind, KindScenario)
	}
	if err := validateName("scenario name", s.Name, maxScenarioName); err != nil {
		fail("%v", err)
	} else if msgs := validation.NameIsDNSLabel(NamespaceFor(pack, s.Name, 999), false); len(msgs) > 0 {
		fail("namespace %q: %s", NamespaceFor(pack, s.Name, 999), strings.Join(msgs, "; "))
	}
	seen := map[string]bool{}
	for _, label := range s.Labels {
		if err := validateLabel(label); err != nil {
			fail("%v", err)
		} else if seen[label] {
			fail("label %q is listed twice", label)
		}
		seen[label] = true
	}
	if s.Priority < 0 {
		fail("priority %d is negative", s.Priority)
	}
	if len(s.Steps) == 0 {
		fail("scenario has no steps")
	}
	for i := range s.Steps {
		if err := s.Steps[i].validate(known, env); err != nil {
			fail("step %d %q: %v", i+1, s.Steps[i].Name, err)
		}
	}
	return errors.Join(errs...)
}

// Validate checks a parsed pack. Templates in the install block are parsed
// here and rendered by the loader.
func (p *OperatorPack) Validate() error {
	var errs []error
	fail := func(format string, args ...any) { errs = append(errs, fmt.Errorf(format, args...)) }
	if p.APIVersion != APIVersion {
		fail("apiVersion %q, want %s", p.APIVersion, APIVersion)
	}
	if p.Kind != KindOperatorPack {
		fail("kind %q, want %s", p.Kind, KindOperatorPack)
	}
	if v := p.Operator.Version; v == "" || strings.ContainsAny(v, " \t\n/") {
		fail("operator.version %q must be a single token without slashes", v)
	}
	if msgs := validation.NameIsDNSLabel(p.Operator.Namespace, false); len(msgs) > 0 {
		fail("operator.namespace %q: %s", p.Operator.Namespace, strings.Join(msgs, "; "))
	}
	if p.Operator.Selector == "" {
		fail("operator.selector is empty")
	} else if _, err := labels.Parse(p.Operator.Selector); err != nil {
		fail("operator.selector: %v", err)
	}
	for i, u := range p.Install.CRDs {
		if !strings.HasPrefix(u, "https://") {
			fail("install.crds[%d] %q must be an https URL", i, u)
		} else if err := checkTemplate(u); err != nil {
			fail("install.crds[%d]: %v", i, err)
		}
	}
	h := p.Install.Helm
	if h == nil {
		fail("install.helm is required")
		return errors.Join(errs...)
	}
	if parsed, err := url.Parse(h.Repo); err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		fail("install.helm.repo %q must be an https URL", h.Repo)
	}
	if h.Chart == "" {
		fail("install.helm.chart is empty")
	}
	if h.Version == "" {
		fail("install.helm.version is empty")
	}
	if msgs := validation.NameIsDNSLabel(h.Release, false); len(msgs) > 0 {
		fail("install.helm.release %q: %s", h.Release, strings.Join(msgs, "; "))
	}
	for k, v := range h.Set {
		if k == "" {
			fail("install.helm.set has an empty key")
		}
		if err := checkTemplate(v); err != nil {
			fail("install.helm.set[%s]: %v", k, err)
		}
	}
	return errors.Join(errs...)
}
