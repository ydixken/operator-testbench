# Environments

A run targets exactly one kubeconfig context, named through `TESTBENCH_CONTEXT`.
In CI and with `task e2e:kind` that context is an ephemeral kind cluster; with `task e2e` it is the context your kubectl is on, after a confirmation prompt.
This page explains the context gate, the single-tenant lock, and the keep and force knobs.
It is filled in with the first release of the engine.
