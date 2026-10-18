# Environments

A run targets exactly one kubeconfig context.
This page explains how a context is chosen and confirmed, what differs between kind and a real cluster, and the knobs a run reads.

## The context gate

The suite's cluster entry point, `TestE2E`, runs only when `TESTBENCH_CONTEXT` names the current kubectl context.
When the variable is unset, the suite skips.
When it names another context, or the kubeconfig cannot be read, the suite fails before it touches anything, with a message that names no context, because CI logs are public.
A pod that runs the suite with its ServiceAccount, where no kubeconfig sets a context, names `in-cluster`.

The Task targets set the variable for you, after showing the context and asking.
Set it yourself only when you call `go test` or the ginkgo CLI directly.
The suite refuses the `-kubeconfig` flag, because `kubectl` and `helm` would not follow it; point it at another kubeconfig with `KUBECONFIG`.

## kind

`task e2e:kind OPERATOR=<pack>` creates the kind cluster `testbench` from `hack/kind/<pack>.yaml` when it does not exist, switches your kubectl context to `kind-testbench`, preloads the pack's images into the cluster, and runs the scenarios unattended with two Ginkgo processes.
It does not prompt: the task selected the context itself a moment earlier, and the unattended precondition still refuses any other current context.

```sh
task e2e:kind OPERATOR=valkey                         # every scenario that is not labelled chaos
task e2e:kind OPERATOR=valkey LABELS=chaos            # the chaos scenarios
task e2e:kind OPERATOR=valkey FOCUS='cluster-ready'   # one scenario
task kind:down                                        # delete the cluster
```

The kind workflow does the same on every pull request, on GitHub's hosted runners, and the nightly workflow runs the chaos scenarios there.

## A real cluster

`task e2e OPERATOR=<pack>` runs against the cluster your kubectl context points at.
It prints the context and asks before anything happens.
The run installs the pack's operator into the pack's namespace, creates one namespace per scenario, and removes both again.

What the identity behind the context needs: to create namespaces, to apply the pack's CRDs (cluster-scoped), to install the chart with its RBAC, and `pods/exec` in the scenario namespaces.
What the cluster needs: image pulls for the operator and the workloads the scenarios apply, and whatever a scenario documents, for example a default StorageClass for a persistence scenario.

> [!warning]
> Never answer the prompt with `task --yes`.
> It would answer every prompt, including one for the wrong cluster.
> A script that cannot answer uses `task e2e:unattended OPERATOR=<pack> EXPECT_CONTEXT=<context>`, which refuses to run unless `EXPECT_CONTEXT` equals the current context.

A multi-cluster setup is covered by running once per context.

## Knobs

Every knob is an environment variable; the Task targets expose them as variables.

| Variable | Task variable | Default | Effect |
|----------|---------------|---------|--------|
| `TESTBENCH_CONTEXT` | set by the task | unset | The gate. MUST equal the current context, or `in-cluster`. |
| `TESTBENCH_OPERATOR` | `OPERATOR` | required | The pack under `operators/`. An unknown pack fails the run. |
| `TESTBENCH_OPERATOR_VERSION` | `VERSION` | the pack's | Overrides the operator version in the CRD URLs and Helm values. |
| `TESTBENCH_OPERATOR_INSTALL` | `INSTALL` | `helm` | `skip` uses an operator already installed in the pack's namespace, and only waits for it. |
| `TESTBENCH_KEEP` | `KEEP` | `false` | `true` leaves the scenario namespaces in place at the end. |
| `TESTBENCH_FORCE` | `FORCE` | `false` | `true` takes over a Helm release a crashed run left behind. |

The Task targets add `FOCUS` (a Ginkgo focus regex), `LABELS` (a Ginkgo label filter), `TIMEOUT` (a Go duration bounding the whole run, default `1h`), and, on `e2e:kind`, `PROCS` (default `2`).

A knob with a value it does not understand fails the run with the value quoted; a typo is never ignored.

## Keeping fixtures

With `KEEP=true`, the scenario namespaces stay, and `task e2e:focus` sets it by default so a rerun finds its namespace and the already applied objects.
The operator is uninstalled regardless, and the next run installs it again before it touches the kept namespaces.
A kept namespace that holds objects with finalizers, such as Valkey's `ValkeyNode`, deletes only while the operator runs: delete it with `task e2e:focus` and `KEEP=false`, or run the operator first.

## What a run leaves behind

Nothing in the scenario namespaces or the operator namespace.
The pack's CRDs stay, on purpose.
On kind, the retained PersistentVolumeClaims of a persistence scenario go with their namespace.
