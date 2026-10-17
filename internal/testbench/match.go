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
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// reportLimit caps a rendered object in a failure message.
const reportLimit = 16 * 1024

// checkSubset is the kuttl-style match: every key the expectation names must
// be present and equal, extra keys in the live object are fine, and a list in
// the expectation is an ordered prefix of the live one. That is exactly what
// apimachinery's DeepDerivative does, so nothing is written here beyond the
// message. The loader already refused the empty values DeepDerivative would
// read as wildcards.
func checkSubset(expected map[string]any, actual *unstructured.Unstructured) error {
	if equality.Semantic.DeepDerivative(expected, actual.Object) {
		return nil
	}
	want, err := yaml.Marshal(expected)
	if err != nil {
		want = []byte(fmt.Sprintf("%v", expected))
	}
	return fmt.Errorf("object lacks the expected subset:\n%s\nactual:\n%s",
		indent(string(want)), indent(ForReport(actual)))
}

func indent(s string) string {
	return "  " + strings.ReplaceAll(strings.TrimRight(s, "\n"), "\n", "\n  ")
}

// ForReport renders an object for a failure message: no managedFields or
// last-applied annotation (noise), no node name (a private fact on a real
// cluster), Secret values replaced by their length, every line redacted, and
// the whole thing capped.
func ForReport(u *unstructured.Unstructured) string {
	c := u.DeepCopy()
	unstructured.RemoveNestedField(c.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(c.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(c.Object, "spec", "nodeName")
	if c.GetKind() == "Secret" {
		for _, field := range []string{"data", "stringData"} {
			values, found, _ := unstructured.NestedMap(c.Object, field)
			if !found {
				continue
			}
			for k, v := range values {
				values[k] = fmt.Sprintf("<redacted %d bytes>", len(fmt.Sprint(v)))
			}
			_ = unstructured.SetNestedMap(c.Object, values, field)
		}
	}
	out, err := yaml.Marshal(c.Object)
	if err != nil {
		return fmt.Sprintf("<cannot render %s %s: %v>", c.GetKind(), c.GetName(), err)
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	for i, line := range lines {
		lines[i] = redactLine(line)
	}
	s := strings.Join(lines, "\n")
	if len(s) > reportLimit {
		s = s[:reportLimit] + "\n... (truncated)"
	}
	return s
}

var (
	redactURL     = regexp.MustCompile(`\b[a-zA-Z][a-zA-Z0-9+.-]*://\S+`)
	redactIPv4    = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)
	redactSecrets = regexp.MustCompile(`(?i)\b(password|passwd|secret|token)\b(["']?\s*[:=]\s*)\S+`)
	redactEnv     = regexp.MustCompile(`\b([A-Z][A-Z0-9_]{2,})=\S+`)
)

// redactLine removes what must not reach a public CI log from one line of
// command output, a log, or a rendered object: URLs (they carry hosts and
// sometimes credentials), IPv4 literals, the value after a password, secret
// or token key, and the value of an UPPER_CASE environment assignment.
func redactLine(s string) string {
	s = redactURL.ReplaceAllString(s, "[redacted url]")
	s = redactIPv4.ReplaceAllString(s, "[redacted ip]")
	s = redactSecrets.ReplaceAllString(s, "$1$2[redacted]")
	s = redactEnv.ReplaceAllString(s, "$1=[redacted]")
	return s
}
