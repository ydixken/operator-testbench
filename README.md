# operator-testbench

<p align="center">
  <a href="https://github.com/ydixken/operator-testbench/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/ydixken/operator-testbench/actions/workflows/ci.yml/badge.svg?branch=main"></a>
  <a href="https://github.com/ydixken/operator-testbench/actions/workflows/e2e-kind.yml"><img alt="E2E kind" src="https://github.com/ydixken/operator-testbench/actions/workflows/e2e-kind.yml/badge.svg?branch=main"></a>
  <a href="https://codecov.io/gh/ydixken/operator-testbench"><img alt="Coverage" src="https://codecov.io/gh/ydixken/operator-testbench/branch/main/graph/badge.svg"></a>
  <a href="go.mod"><img alt="Go version" src="https://img.shields.io/github/go-mod/go-version/ydixken/operator-testbench"></a>
  <a href="LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue"></a>
</p>

An e2e testbench for Kubernetes operators: pick an operator pack, write scenarios as YAML, and run them as Ginkgo specs against a kind cluster or the kubectl context you are on.
The first pack covers [valkey-operator](https://github.com/valkey-io/valkey-operator).

**Documentation:** [ydixken.github.io/operator-testbench](https://ydixken.github.io/operator-testbench/)

> [!important]
> In development, pre-1.0.
> The scenario schema is `testbench.dev/v1alpha1` and can change.

## Quickstart

```sh
brew install go task kind golangci-lint yamllint actionlint   # or your package manager
task e2e:kind OPERATOR=valkey      # creates the kind cluster 'testbench', installs the operator, runs the scenarios
task kind:down                     # deletes the cluster
```

To run against the cluster your kubectl context points at, `task e2e OPERATOR=valkey` prints the context and asks first.
The [quickstart](docs/quickstart.md) walks through a first run and reading its report; [environments](docs/environments.md) covers real clusters and the safety rules.

## Structure

```sh
internal/testbench/        # the engine: schema, loader, matchers, CEL, steps, install, diagnostics (Gomega level, no Ginkgo)
operators/<pack>/          # one operator pack: operator.yaml (how to install), manifests/, scenarios/*.yaml
operators/valkey/          # the first pack, valkey-operator
test/e2e/                  # the Ginkgo suite: TestE2E gate, spec tree from the embedded packs, registry of Go steps
hack/kind/                 # kind cluster configs and the image preload script
docs/                      # user docs, rendered to a site by mkdocs (mkdocs.yml)
.claude/skills/            # vendored skills (ponytail, humanizer, brainstorming, asd-ste100), mandatory per AGENTS.md
.agents/skills/            # the same skills for agents that read the vendor-neutral location
.github/workflows/         # CI (lint, test, docs), kind e2e per pull request, nightly chaos, docs deploy, release
Dockerfile                 # the testbench image: compiled suite, ginkgo CLI, kubectl (distroless, no shell)
Taskfile.yml               # task help | lint | test | kind:up | kind:down | e2e | e2e:kind | image:build
```

## Developing

```sh
task help      # list tasks
task lint      # the pre-commit gate (YAML, workflows, docs links, Go)
task test      # unit tests without a cluster; parses every embedded pack
task e2e:kind OPERATOR=valkey             # the full non-chaos suite on local kind, two processes
task e2e:kind OPERATOR=valkey LABELS=chaos # the chaos scenarios
task e2e OPERATOR=valkey                  # against your CURRENT kubectl context (prompts first)
```

## Documentation

- [Quickstart](docs/quickstart.md): a first run on kind.
- [Concepts](docs/concepts.md): packs, scenarios, steps, environments, labels.
- [Writing scenarios](docs/scenarios.md): the step kinds, templating, timeouts, cleanup.
- [Operator packs](docs/packs.md): how a pack installs and pins an operator.
- [Valkey pack](docs/operators/valkey.md): the shipped scenarios and what each needs.
- [Environments](docs/environments.md): kind, real contexts, the context gate, keep and force knobs.
- [Reference](docs/reference/scenario-schema.md): the scenario schema, the pack schema, environment variables, Task targets.
- [Troubleshooting](docs/troubleshooting.md): symptoms mapped to causes and fixes.
- [Authors](AUTHORS.md): who wrote this.
- [CONTRIBUTING.md](CONTRIBUTING.md): how to work here.
- [AGENTS.md](AGENTS.md): rules for AI agents, including the mandatory skills.
- [LICENSE](LICENSE): Apache-2.0.
