# Quickstart

A first run on a local kind cluster.
You need Docker running, plus `go`, `task`, `kind`, `kubectl`, and `helm` on your machine (`brew install go task kind kubectl helm` on a machine with Homebrew).

```sh
git clone https://github.com/ydixken/operator-testbench
cd operator-testbench
task e2e:kind OPERATOR=valkey
```

The task creates the kind cluster `testbench` (one control plane, two workers), switches your kubectl context to `kind-testbench`, preloads the Valkey images into the cluster, installs valkey-operator at the version the pack pins, and runs every scenario that is not labelled `chaos` across two Ginkgo processes.
A full run takes 20 to 40 minutes on a laptop; most of it is waiting for Valkey clusters to form.

The Ginkgo report goes to `reports/report.json` and `reports/junit.xml`.
A failed step prints the namespace's pods and events and the operator's log tail as report entries.

```sh
task e2e:kind OPERATOR=valkey FOCUS='cluster-ready'   # one scenario
task e2e:kind OPERATOR=valkey LABELS=chaos            # the chaos scenarios
task kind:down                                        # delete the cluster
```

To run against the cluster your kubectl context already points at, use `task e2e OPERATOR=valkey`.
It prints the context and asks before touching anything; [Environments](environments.md) explains the rules that apply to a real cluster.
