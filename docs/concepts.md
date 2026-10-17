# Concepts

This page explains the vocabulary the rest of the documentation uses.
It is filled in with the first release of the engine.

- An **operator pack** is a directory under `operators/` that says how to install one pinned version of an operator and holds that operator's scenarios.
- A **scenario** is one YAML file under the pack's `scenarios/` directory: an ordered list of steps that run in their own namespace.
- A **step** is one action with its own timeout: `apply`, `assert`, `exec`, `delete`, or `go`.
- An **environment** is the one kubeconfig context a run targets, named explicitly through `TESTBENCH_CONTEXT`.
- **Labels** select scenarios with Ginkgo's label filter; `chaos` has a fixed meaning and runs nightly only.
