# Environment variables

The engine reads these variables once, at the start of a run.
A value the engine does not accept stops the run.
The error quotes the value.

| Variable | Default | Rule |
|----------|---------|------|
| `TESTBENCH_CONTEXT` | unset | The gate. The value must be the current kubectl context, or `in-cluster` when no kubeconfig sets a context. When the variable is unset, `TestE2E` skips. The Task targets set it. |
| `TESTBENCH_OPERATOR` | unset | Required when the gate is open. The pack name under `operators/`. A DNS label of 16 characters or less. An unknown pack stops the run. |
| `TESTBENCH_OPERATOR_VERSION` | the pack's `operator.version` | Replaces the operator version. One token, without spaces or `/`. |
| `TESTBENCH_OPERATOR_INSTALL` | `helm` | `helm` installs the chart. `skip` does not install, and waits for a Ready controller pod in the pack's namespace. |
| `TESTBENCH_KEEP` | `false` | `true` keeps the scenario namespaces at the end. `false` deletes them. Other values stop the run. |
| `TESTBENCH_FORCE` | `false` | `true` takes over a Helm release that an earlier run left, and replaces CRDs that another install manages. `false` stops the run when the release exists or when another install manages a CRD. Other values stop the run. |

The Task targets map these variables to `OPERATOR`, `VERSION`, `INSTALL`, `KEEP` and `FORCE`.
The Task targets also accept `FOCUS`, `LABELS`, `TIMEOUT` and, for `e2e:kind`, `PROCS`.
Those four are not read by the engine.
They become flags of `go test` or of the ginkgo CLI.

`KUBECONFIG` selects the kubeconfig file.
The engine refuses the flag `-kubeconfig`, because `kubectl` and `helm` do not read it.
