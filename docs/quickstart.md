# Quickstart

A first run on a local kind cluster.
You need Docker running, plus `go`, `task`, `kind`, `kubectl`, and `helm` on your machine.
On a machine with Homebrew: `brew install go task kind kubectl helm`.

```sh
git clone https://github.com/ydixken/operator-testbench
cd operator-testbench
task e2e:kind OPERATOR=valkey
```

The task creates the kind cluster `testbench` (one control plane, two workers), switches your kubectl context to `kind-testbench`, preloads the Valkey images into the cluster, installs valkey-operator at the version the pack pins, and runs every scenario that is not labelled `chaos` across two Ginkgo processes.
A full run takes 15 to 30 minutes on a laptop; most of it is waiting for Valkey clusters to form and for slots to migrate.

Each scenario shows up as one Ginkgo container, each step as one spec:

```text
cluster-ready 01 apply a 3x1 cluster
cluster-ready 02 the cluster reports Ready for the applied generation
...
Ran 19 of 19 Specs in 66.820 seconds
SUCCESS! -- 19 Passed | 0 Failed | 0 Pending | 0 Skipped
```

The Ginkgo report goes to `reports/report.json` and `reports/junit.xml`.
A failed step prints the namespace's pods and events and the operator's log tail as report entries.

```sh
task e2e:kind OPERATOR=valkey FOCUS='cluster-ready'   # one scenario
task e2e:kind OPERATOR=valkey LABELS=chaos            # the chaos scenarios
task kind:down                                        # delete the cluster
```

To run against the cluster your kubectl context already points at, use `task e2e OPERATOR=valkey`.
It prints the context and asks before touching anything; [Environments](environments.md) explains the rules that apply to a real cluster.

Next: [Concepts](concepts.md) for the vocabulary, [Writing scenarios](scenarios.md) to add a scenario, [Operator packs](packs.md) to add an operator.
