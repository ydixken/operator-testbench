# Concepts

The vocabulary the rest of the documentation uses, and the rules behind it.

## Operator pack

A pack is a directory under `operators/`, named after the operator it tests.
It holds one `operator.yaml` that says how to install one pinned version of that operator, the manifests its scenarios apply, and the scenarios themselves.
The pack is YAML only.
A pack says nothing about clusters: the same pack runs on kind in CI and on whatever kubectl context you point it at.

The engine installs the operator once per run, from the pack's Helm chart, after applying the pack's CRD manifests server-side.
It installs into the namespace the pack names, which the engine creates.
When the pack names the chart's watch-namespaces value, the installed operator reconciles only the scenario namespaces.
CRDs are cluster-wide, so the engine refuses to replace CRDs that another install of the operator owns.

## Scenario

A scenario is one YAML file under the pack's `scenarios/` directory.
Its file name is its name.
It is an ordered list of steps, and it runs in a namespace of its own: `testbench-<pack>-<scenario>` on Ginkgo process 1, with a `-<N>` suffix on process N.
Every object a scenario applies without a namespace lands in that namespace, and the engine deletes the objects and then the namespace when the scenario ends, whether it passed or failed.

A scenario starts from an empty namespace and MUST NOT depend on what another scenario left behind.

## Step

A step is one action with its own timeout.
There are five kinds, and a step has exactly one of them:

- `apply` server-side applies every object in a manifest file of the pack.
  Applying a second version of the same object is how a scenario changes a spec; there is no patch step.
- `assert` polls an object or a list until it looks as expected, or the timeout passes.
  The expectation is a subset the object must contain, CEL expressions, a count, or absence.
- `exec` runs a command in a container and polls until its output contains every expected string.
- `delete` removes an object or everything a selector matches, and waits until it is gone.
- `go` calls a Go function registered in `test/e2e` and stores what it returns for later steps.

[Writing scenarios](scenarios.md) explains each kind; the [scenario schema](reference/scenario-schema.md) lists every field.

## Environment

A run targets exactly one kubeconfig context, named through `TESTBENCH_CONTEXT`.
The suite refuses to start when that variable is unset, or names a context other than the current one, so a plain `go test ./...` can never reach a cluster nobody named.
A multi-cluster setup is covered by running once per context.
[Environments](environments.md) describes kind, real clusters, and the knobs.

## Labels and priority

Labels are Ginkgo labels.
The pack name is added to every scenario automatically, so `--label-filter=valkey` selects a pack.
`chaos` has a fixed meaning: scenarios that delete pods or otherwise disturb the system under test.
They are excluded from `task e2e`, `task e2e:kind`, and the pull request gate, and the nightly workflow runs them.

`priority` becomes Ginkgo's `SpecPriority`: a higher value starts earlier when several processes run in parallel, so the long scenarios do not end up last.

## Where the logic lives

The YAML is data.
The logic that runs it is Go: the engine under `internal/testbench` parses, validates, and executes steps, and the Ginkgo suite under `test/e2e` turns every scenario into an `Ordered` container and every step into an `It`.
Ginkgo gives the run its parallelism, its labels, its reports, and its timeouts; the YAML gives it the content.
