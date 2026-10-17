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
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// diagTimeout bounds each read a diagnostic makes; a dead API server must
	// not hold the failure report hostage.
	diagTimeout = 30 * time.Second
	// diagEvents is how many of the newest events a namespace report shows.
	diagEvents = 50
	// diagLogBytes caps the controller log tail.
	diagLogBytes = "65536"
)

// Diagnostics reads the story of a failed step: what the namespace looks like
// and what the controller said. Every read reports and never asserts, so a
// diagnostic that fails cannot replace the real failure.
type Diagnostics struct {
	Client  client.Client
	Context string
	Command CommandFactory
	Pack    *Pack
}

// eventTime is when an event happened: LastTimestamp for the core API, which
// the events API leaves empty in favour of EventTime.
func eventTime(e corev1.Event) time.Time {
	if !e.LastTimestamp.IsZero() {
		return e.LastTimestamp.Time
	}
	return e.EventTime.Time
}

// Namespace lists the pods with their phase, restarts and container states,
// then the newest events in time order. No node names, no IPs.
func (d *Diagnostics) Namespace(ctx context.Context, ns string) string {
	ctx, cancel := context.WithTimeout(ctx, diagTimeout)
	defer cancel()
	var b strings.Builder
	var pods corev1.PodList
	if err := d.Client.List(ctx, &pods, client.InNamespace(ns)); err != nil {
		fmt.Fprintf(&b, "pods: cannot list: %v\n", redactLine(err.Error()))
	} else {
		fmt.Fprintf(&b, "pods in %s: %d\n", ns, len(pods.Items))
		for _, p := range pods.Items {
			restarts := int32(0)
			var states []string
			for _, cs := range p.Status.ContainerStatuses {
				restarts += cs.RestartCount
				switch {
				case cs.State.Waiting != nil:
					states = append(states, cs.Name+" waiting "+cs.State.Waiting.Reason)
				case cs.State.Terminated != nil:
					states = append(states, cs.Name+" terminated "+cs.State.Terminated.Reason+
						" exit "+strconv.Itoa(int(cs.State.Terminated.ExitCode)))
				case cs.State.Running != nil && !cs.Ready:
					states = append(states, cs.Name+" running, not ready")
				}
			}
			fmt.Fprintf(&b, "  %s %s restarts=%d", p.Name, p.Status.Phase, restarts)
			if len(states) > 0 {
				fmt.Fprintf(&b, " (%s)", strings.Join(states, "; "))
			}
			b.WriteString("\n")
		}
	}
	var events corev1.EventList
	if err := d.Client.List(ctx, &events, client.InNamespace(ns)); err != nil {
		fmt.Fprintf(&b, "events: cannot list: %v\n", redactLine(err.Error()))
		return b.String()
	}
	slices.SortFunc(events.Items, func(a, c corev1.Event) int { return eventTime(a).Compare(eventTime(c)) })
	if len(events.Items) > diagEvents {
		fmt.Fprintf(&b, "events: newest %d of %d\n", diagEvents, len(events.Items))
		events.Items = events.Items[len(events.Items)-diagEvents:]
	} else {
		fmt.Fprintf(&b, "events: %d\n", len(events.Items))
	}
	for _, e := range events.Items {
		line := fmt.Sprintf("  %s %s %s %s/%s: %s", eventTime(e).UTC().Format(time.TimeOnly), e.Type, e.Reason,
			e.InvolvedObject.Kind, e.InvolvedObject.Name, strings.TrimSpace(e.Message))
		b.WriteString(redactLine(line) + "\n")
	}
	return b.String()
}

// ControllerLogs is the redacted tail of every controller container the pack's
// selector matches, with the pod prefixed on each line.
func (d *Diagnostics) ControllerLogs(ctx context.Context, lines int) string {
	ctx, cancel := context.WithTimeout(ctx, diagTimeout)
	defer cancel()
	args := kubectlArgs(d.Context, "logs", "-n", d.Pack.Spec.Operator.Namespace, "-l", d.Pack.Spec.Operator.Selector,
		"--all-containers", "--prefix", "--tail="+strconv.Itoa(lines), "--limit-bytes="+diagLogBytes)
	out, err := d.Command(ctx, "kubectl", args...).CombinedOutput()
	text := redactLines(string(out))
	if err != nil {
		return fmt.Sprintf("controller logs: kubectl logs failed: %v\n%s", err, text)
	}
	return text
}
