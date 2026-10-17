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
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// The run's knobs. TESTBENCH_CONTEXT is read by the gate in test/e2e; the
// rest are parsed here. Every bad value is quoted back, so a typo is found in
// the message, and a bad knob fails before any cluster call.
const (
	EnvContext  = "TESTBENCH_CONTEXT"
	EnvOperator = "TESTBENCH_OPERATOR"
	EnvVersion  = "TESTBENCH_OPERATOR_VERSION"
	EnvInstall  = "TESTBENCH_OPERATOR_INSTALL"
	EnvKeep     = "TESTBENCH_KEEP"
	EnvForce    = "TESTBENCH_FORCE"

	// InstallHelm installs the pack's chart; InstallSkip expects the operator
	// to be there already and only waits for it.
	InstallHelm = "helm"
	InstallSkip = "skip"
)

// Env is the parsed configuration of one run.
type Env struct {
	// Context is TESTBENCH_CONTEXT as given; the gate compares it.
	Context string
	// Operator is the pack name under operators/.
	Operator string
	// Version overrides the pack's operator version when set.
	Version string
	// Install is InstallHelm or InstallSkip.
	Install string
	// Keep leaves the scenario namespaces in place at the end.
	Keep bool
	// Force takes over a Helm release a crashed run left behind.
	Force bool
}

// ParseEnv reads the knobs through getenv (os.Getenv in production, a map in
// tests). It expects to be called only once the gate has opened, so the pack
// name is required.
func ParseEnv(getenv func(string) string) (Env, error) {
	e := Env{
		Context:  getenv(EnvContext),
		Operator: getenv(EnvOperator),
		Version:  getenv(EnvVersion),
		Install:  getenv(EnvInstall),
	}
	var errs []error
	if e.Operator == "" {
		errs = append(errs, errors.New(EnvOperator+" is required: name a pack under operators/"))
	} else if err := validateName(EnvOperator, e.Operator, maxPackName); err != nil {
		errs = append(errs, err)
	}
	if strings.ContainsAny(e.Version, " \t\n/") {
		errs = append(errs, fmt.Errorf("%s=%s must be a single token without slashes", EnvVersion, strconv.Quote(e.Version)))
	}
	switch e.Install {
	case "":
		e.Install = InstallHelm
	case InstallHelm, InstallSkip:
	default:
		errs = append(errs, fmt.Errorf("%s=%s must be %s or %s",
			EnvInstall, strconv.Quote(e.Install), InstallHelm, InstallSkip))
	}
	var err error
	if e.Keep, err = parseBool(EnvKeep, getenv(EnvKeep)); err != nil {
		errs = append(errs, err)
	}
	if e.Force, err = parseBool(EnvForce, getenv(EnvForce)); err != nil {
		errs = append(errs, err)
	}
	return e, errors.Join(errs...)
}

// parseBool accepts exactly "true", "false" and unset. A knob that is set to
// anything else is a misconfiguration, which must be red rather than ignored.
func parseBool(name, value string) (bool, error) {
	switch value {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	default:
		return false, fmt.Errorf("%s=%s must be true or false", name, strconv.Quote(value))
	}
}
