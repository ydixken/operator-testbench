# operator-testbench

An e2e testbench for Kubernetes operators.
You pick an operator pack, write scenarios as YAML, and the testbench runs them as Ginkgo specs against a kind cluster or the kubectl context you are on.
The scenarios are the testbench's own catalog for each operator; the operator's upstream test suite is not what runs here.

**In development, pre-1.0.** The scenario schema is `testbench.dev/v1alpha1` and can change.
The first pack covers [valkey-operator](https://github.com/valkey-io/valkey-operator).

Where to go:

- [Quickstart](quickstart.md): a first run on kind, end to end.
- [Concepts](concepts.md): packs, scenarios, steps, environments, labels, and the one-context-per-run rule.
- [Writing scenarios](scenarios.md): the step kinds, templating, timeouts, and cleanup.
- [Operator packs](packs.md): how a pack installs and pins an operator.
- [Valkey](operators/valkey.md): the shipped scenarios and what each needs.
- [Environments](environments.md): kind, real contexts, the context gate, and the keep and force knobs.
- [Scenario schema](reference/scenario-schema.md), [pack schema](reference/pack-schema.md), [environment variables](reference/environment.md), [Task targets](reference/tasks.md): the reference pages.
- [Troubleshooting](troubleshooting.md): symptoms mapped to causes and fixes.
- [Contributing](contributing.md): where the contributor and agent guides live.
