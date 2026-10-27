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
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"
	"text/template"

	"sigs.k8s.io/yaml"
)

const (
	packFile     = "operator.yaml"
	scenariosDir = "scenarios"
)

// Pack is a loaded operator pack: its spec, the resolved version, the install
// inputs rendered with that version, and every scenario in file order.
type Pack struct {
	Name string
	Spec OperatorPack
	// Version is the override when one was given, else the pack's own.
	Version string
	// CRDs and HelmSet are the pack's install.crds and install.helm.set with
	// {{ .Version }} rendered.
	CRDs    []string
	HelmSet map[string]string
	// Scenarios are sorted by file name, so every Ginkgo process builds the
	// same spec tree from them.
	Scenarios []Scenario
	// FS is the packs file system; manifests live at path.Join(Name, file).
	FS fs.FS
}

// Namespaces lists every scenario namespace of a run on procs Ginkgo
// processes, the same names NamespaceFor gives each process.
func (p *Pack) Namespaces(procs int) []string {
	names := make([]string, 0, max(procs, 1)*len(p.Scenarios))
	for proc := 1; proc <= max(procs, 1); proc++ {
		for _, s := range p.Scenarios {
			names = append(names, NamespaceFor(p.Name, s.Name, proc))
		}
	}
	return names
}

// TemplateData is what a scenario's templated fields see.
type TemplateData struct {
	// Namespace is the scenario's namespace on this process.
	Namespace string
	// Version is the resolved operator version.
	Version string
	// Vars holds what Go steps returned so far.
	Vars map[string]string
}

// Render executes tmpl with data; a missing key or field is an error, never
// an empty string that would silently reach a cluster.
func Render(tmpl string, data any) (string, error) {
	t, err := template.New("").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ListPacks names every top-level directory of fsys that holds an
// operator.yaml, sorted.
func ListPacks(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := fs.Stat(fsys, path.Join(e.Name(), packFile)); err == nil {
			names = append(names, e.Name())
		}
	}
	return names, nil
}

// LoadPack reads, validates and renders the pack named name from fsys. known
// holds the registered Go step names. Every broken file is reported together,
// so one run shows every problem.
func LoadPack(fsys fs.FS, name, versionOverride string, known map[string]bool) (*Pack, error) {
	if err := validateName("pack name", name, maxPackName); err != nil {
		return nil, err
	}
	specPath := path.Join(name, packFile)
	data, err := fs.ReadFile(fsys, specPath)
	if err != nil {
		return nil, fmt.Errorf("pack %q: %w", name, err)
	}
	p := &Pack{Name: name, FS: fsys}
	if err := yaml.UnmarshalStrict(data, &p.Spec); err != nil {
		return nil, fmt.Errorf("%s: %w", specPath, err)
	}
	if err := p.Spec.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", specPath, err)
	}
	p.Version = p.Spec.Operator.Version
	if versionOverride != "" {
		p.Version = versionOverride
	}
	if err := p.render(specPath); err != nil {
		return nil, err
	}
	env, err := NewCELEnv()
	if err != nil {
		return nil, err
	}
	if err := p.loadScenarios(known, env); err != nil {
		return nil, err
	}
	return p, nil
}

// render fills the install inputs. Only the version is in scope here: a
// namespace or a var belongs to a scenario, and asking for one in
// operator.yaml is a mistake the loader points out.
func (p *Pack) render(specPath string) error {
	data := struct{ Version string }{p.Version}
	p.CRDs = make([]string, 0, len(p.Spec.Install.CRDs))
	for i, tmpl := range p.Spec.Install.CRDs {
		s, err := Render(tmpl, data)
		if err != nil {
			return fmt.Errorf("%s: install.crds[%d]: %w", specPath, i, err)
		}
		p.CRDs = append(p.CRDs, s)
	}
	p.HelmSet = make(map[string]string, len(p.Spec.Install.Helm.Set))
	for k, tmpl := range p.Spec.Install.Helm.Set {
		s, err := Render(tmpl, data)
		if err != nil {
			return fmt.Errorf("%s: install.helm.set[%s]: %w", specPath, k, err)
		}
		p.HelmSet[k] = s
	}
	return nil
}

func (p *Pack) loadScenarios(known map[string]bool, env *CELEnv) error {
	dir := path.Join(p.Name, scenariosDir)
	entries, err := fs.ReadDir(p.FS, dir) // sorted by name
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("pack %q: %w", p.Name, err)
	}
	var errs []error
	for _, e := range entries {
		// Hidden entries are not scenarios: a mounted ConfigMap carries ..data
		// and a timestamped directory, a checkout may carry .gitkeep.
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		file := path.Join(dir, e.Name())
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			errs = append(errs, fmt.Errorf("%s: only *.yaml scenario files belong here", file))
			continue
		}
		sc, err := p.loadScenario(file, strings.TrimSuffix(e.Name(), ".yaml"), known, env)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		p.Scenarios = append(p.Scenarios, *sc)
	}
	if len(errs) == 0 && len(p.Scenarios) == 0 {
		errs = append(errs, fmt.Errorf("pack %q has no scenarios under %s", p.Name, dir))
	}
	return errors.Join(errs...)
}

func (p *Pack) loadScenario(file, name string, known map[string]bool, env *CELEnv) (*Scenario, error) {
	data, err := fs.ReadFile(p.FS, file)
	if err != nil {
		return nil, err
	}
	var sc Scenario
	if err := yaml.UnmarshalStrict(data, &sc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	sc.Name, sc.Path = name, file
	if err := sc.Validate(p.Name, known, env); err != nil {
		return nil, err
	}
	// The manifest has to exist now, not when the step runs on a cluster.
	for i := range sc.Steps {
		if a := sc.Steps[i].Apply; a != nil {
			if _, err := fs.Stat(p.FS, path.Join(p.Name, a.File)); err != nil {
				return nil, fmt.Errorf("%s: step %d %q: apply.file %q: %w", file, i+1, sc.Steps[i].Name, a.File, err)
			}
		}
	}
	return &sc, nil
}
