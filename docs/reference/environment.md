# Environment variables

The engine reads these variables once, at the start of a run.
A value the engine does not accept stops the run.
The error quotes the value.

| Variable | Default | Rule |
|----------|---------|------|
| `TESTBENCH_CONTEXT` | unset | The gate. The value must be the current kubectl context, or `in-cluster` when no kubeconfig sets a context. When the variable is unset, `TestE2E` skips. The Task targets set it. |
| `TESTBENCH_OPERATOR` | unset | Required when the gate is open. The pack name under `operators/`. A DNS label of 16 characters or less. An unknown pack stops the run. |
| `TESTBENCH_OPERATOR_VERSION` | the pack's `operator.version` | Replaces the operator version. One token, without spaces or `/`. |
| `TESTBENCH_OPERATOR_INSTALL` | `helm` | `helm` installs the chart. `skip` does not install, and waits for a Ready controller pod in the operator namespace. In the two modes, each Ready controller pod must run the operator version. |
| `TESTBENCH_OPERATOR_NAMESPACE` | the pack's `operator.namespace` | The namespace of the operator. With `TESTBENCH_OPERATOR_INSTALL=skip`, the namespace where the installed operator runs. A DNS label. |
| `TESTBENCH_NAMESPACES` | `managed` | `managed`: the engine creates and deletes the scenario namespaces. `provided`: the scenario namespaces exist before the run. The engine does not create or delete them. Each namespace must be empty of pods, PersistentVolumeClaims and the kinds that the scenario applies. Other values stop the run. |
| `TESTBENCH_KEEP` | `false` | `true` keeps the scenario namespaces at the end. `false` deletes them. Other values stop the run. |
| `TESTBENCH_FORCE` | `false` | `true` takes over a Helm release that an earlier run left, and replaces CRDs that another install manages. `false` stops the run when the release exists or when another install manages a CRD. With `TESTBENCH_NAMESPACES=provided`, `true` also deletes the objects that an earlier run left in a scenario namespace. Other values stop the run. |

The Task targets map these variables to `OPERATOR`, `VERSION`, `INSTALL`, `OPERATOR_NAMESPACE`, `NAMESPACES`, `KEEP` and `FORCE`.
The Task targets also accept `FOCUS`, `LABELS`, `TIMEOUT` and, for `e2e:kind`, `PROCS`.
Those four are not read by the engine.
They become flags of `go test` or of the ginkgo CLI.

`KUBECONFIG` selects the kubeconfig file.
The engine refuses the flag `-kubeconfig`, because `kubectl` and `helm` do not read it.
