# CONTRIBUTING.md

The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are to be interpreted as described in RFC 2119.

[TOC]

## Requirements

- [go](https://go.dev): the testbench's language and test toolchain.
- [task](https://taskfile.dev): task runner, the entrypoint for everything.
- [yamllint](https://yamllint.readthedocs.io): lints all YAML, packs and scenarios included.
- [golangci-lint](https://golangci-lint.run) v2: Go linting, at the version in `.golangci-lint-version`.
- [kubectl](https://kubernetes.io/docs/reference/kubectl/), [helm](https://helm.sh) and [kind](https://kind.sigs.k8s.io): the e2e tasks shell out to them.
  kind needs a reachable Docker daemon.
- [gh](https://cli.github.com): PRs happen on GitHub.
- [actionlint](https://github.com/rhysd/actionlint) MAY be installed; `task lint` runs it when present and CI always does.

On a machine with Homebrew: `brew install go task kind golangci-lint yamllint actionlint`.

## Day-to-day loop

1. Branch from `main`.
1. Make one logical change.
1. Format touched files before every commit and run `task lint`.
1. Run `task test`: it parses every embedded pack, so a broken scenario fails here, before a cluster is involved.
1. Commit (see below), push the branch to GitHub, open a PR.
1. Merge when the CI `lint`, `test`, `docs`, `e2e-kind`, and `e2e-kind-cluster` checks are green on the current head.

`.github/workflows/ci.yml` runs lint, tests and the docs build on every push and pull request, `.github/workflows/e2e-kind.yml` runs the full non-chaos suite on an ephemeral kind cluster, and `.github/workflows/e2e-kind-cluster.yml` deploys the image and the chart in cluster mode against an upstream-installed operator.
All five MUST be green before a merge.
The branch protection enforces the first four; `e2e-kind-cluster` is checked by hand until a repository admin adds it.
The pull request `lint` job runs GitHub Dependency Review and rejects new dependencies with moderate or higher known vulnerabilities, disallowed licenses, or violations in runtime, development, or unknown scopes.
GitHub cannot fail Dependency Review for every unresolved license, so contributors MUST review those warnings and resolve each license from a public source before merging.

Coverage goes to Codecov, gated on the `CODECOV_TOKEN` repository secret.
Codecov rejects tokenless uploads even from public repositories, so without the secret the upload step skips visibly rather than passing quietly; with it set, a failed upload fails the job.
The coverage total is printed in the job summary either way.

## Workflows and fork safety

Every job runs on GitHub-hosted `ubuntu-latest` runners.
The kind cluster a run creates lives and dies inside that runner, so a fork's pull request can run the full suite with a read-only token and no secrets.

> [!warning]
> Two rules hold for every workflow in this repository:
>
> - No workflow MAY target a privileged runner or environment on a code path a fork can reach.
> - `pull_request_target` MUST NOT be used in any workflow.
>   It runs the base branch's copy of the workflow, with the base branch's secrets, against a fork's code.

The CI `lint` job runs [actionlint](https://github.com/rhysd/actionlint) on all workflows; yamllint alone cannot detect a step that lost its `uses:`.
Local `task lint` runs actionlint when installed and prints a skip message otherwise.

## E2e tests

`task e2e OPERATOR=<pack>` runs the pack's scenarios against the CURRENT kubectl context, a real cluster; it prints the context and prompts before touching anything (see the Caution section in [AGENTS.md](AGENTS.md)).
Every e2e Task target then passes the context it confirmed to the suite as `TESTBENCH_CONTEXT`, assigned in the command itself because Task lets an exported variable override a Taskfile `env:` value.
The cluster entry point `TestE2E` skips when `TESTBENCH_CONTEXT` is unset, and fails before any cluster call when it names a context other than `kubectl config current-context`.
We make the context an explicit input because the suite installs an operator and deletes namespaces, so a plain `go test ./test/e2e` or `go test ./...` must never reach a cluster that nobody named.

`task e2e:kind OPERATOR=<pack>` is the everyday path.
It creates the kind cluster `testbench` from `hack/kind/<pack>.yaml` when it does not exist, selects the context `kind-testbench`, preloads the pack's workload images into the cluster, and runs the suite unattended with two Ginkgo processes.
`task kind:down` deletes the cluster.

A run installs the pack's operator once, on Ginkgo process 1, before any scenario: it applies the CRD manifests the pack names with `kubectl apply --server-side`, installs the Helm chart at the pinned version with `--skip-crds --wait`, and waits until a pod matching the pack's controller selector is Ready and every Ready one runs the pack's version as its image tag.
It refuses a CRD that exists without its field manager, because another install of the operator owns it, and with `install.helm.watchNamespacesValue` the installed operator watches only the scenario namespaces.
The suite is single-tenant per cluster: `helm status` on the pack's release name is the lock, and a release that already exists fails the run unless `TESTBENCH_FORCE=true` takes it over.
Every scenario then runs in its own namespace, `testbench-<pack>-<scenario>` on process 1 and with a `-<N>` suffix on process N, and deletes what it applied and then the namespace when it ends.
After the last scenario, process 1 deletes every remaining scenario namespace while the operator still runs (an operator's finalizers need a live controller), then uninstalls the operator.
The CRDs stay installed: deleting a CRD deletes every object of that kind cluster-wide.

A run reads these environment variables, each shown with the default it takes when unset:

- `TESTBENCH_CONTEXT` (unset) MUST name the current kubectl context, or be `in-cluster` when no kubeconfig sets one, for `TestE2E` to run.
  The Task targets set it; set it yourself only when you call `go test` or the ginkgo CLI directly.
  Point the suite at another kubeconfig with `KUBECONFIG`: `TestE2E` refuses the `-kubeconfig` flag, because the suite's `kubectl` and `helm` calls would not follow it.
- `TESTBENCH_OPERATOR` (unset) names the pack under `operators/` to run.
  Required once `TESTBENCH_CONTEXT` opens the gate; an unknown pack fails the run rather than skipping it, because a skipped suite exits 0.
- `TESTBENCH_OPERATOR_VERSION` (the pack's `operator.version`) overrides the operator version: the CRD URLs and the Helm values the pack templates with `{{ .Version }}` follow it.
  It is a patch-level knob; a minor that changes the API needs the chart version, the CRDs, and the scenarios bumped together in one PR.
- `TESTBENCH_OPERATOR_INSTALL` (`helm`) set to `skip` uses an operator that is already installed on the context, and only waits for a pod matching the pack's selector in the operator namespace, running the pack's version.
- `TESTBENCH_OPERATOR_NAMESPACE` (the pack's `operator.namespace`) is the operator namespace; with `skip`, where the installed operator runs.
- `TESTBENCH_NAMESPACES` (`managed`) set to `provided` means the scenario namespaces exist before the run: the engine never creates or deletes them, and refuses one that is not empty unless `TESTBENCH_FORCE=true` clears it.
- `TESTBENCH_PACK_DIR` (unset) names an absolute directory laid out like `operators/`; its packs replace the embedded ones, and `task pack:validate PACK_DIR=<dir>` checks it without a cluster.
- `TESTBENCH_KEEP` (unset) keeps the scenario namespaces when `true`, for iteration; the operator is uninstalled regardless, and the next run installs it again before it touches the kept namespaces.
  A kept namespace that holds objects with finalizers deletes only while the operator runs.
- `TESTBENCH_FORCE` (unset) takes over the Helm release a crashed run left behind when `true`, and replaces CRDs another install owns.

The Task targets expose these as `VERSION=`, `INSTALL=`, `OPERATOR_NAMESPACE=`, `NAMESPACES=`, `KEEP=`, and `FORCE=`, plus `FOCUS=` (a Ginkgo focus regex), `TIMEOUT=` (a Go duration bounding the whole run, default `1h`), and `PROCS=` on `e2e:kind` (default `2`).
`-ginkgo.timeout` always equals `-timeout`, because Ginkgo's own suite timeout defaults to 1h and would abort a long run on its own.

Scenarios labelled `chaos` delete pods and are excluded from `task e2e`, `task e2e:kind`, and the pull request gate (`--label-filter='!chaos'`).
`task e2e:chaos OPERATOR=<pack>` runs exactly them, with the same context echo and confirmation prompt; `task e2e:chaos:unattended` drops the prompt and requires `EXPECT_CONTEXT`.
`task e2e:kind OPERATOR=<pack> LABELS=chaos` runs them on kind.
The nightly workflow runs them on kind and opens or updates an issue labelled `nightly-failure` when they fail.

A scenario that needs something only some clusters have (a default StorageClass, more than one worker, permission to mutate nodes) MUST say so in its header comment and in `docs/operators/<pack>.md`.

### How does a parallel run work?

`task e2e:kind` and the kind workflows run the suite with `--procs` through the ginkgo CLI, which `go run github.com/onsi/ginkgo/v2/ginkgo` builds at the version `go.mod` pins.
`go test` cannot run Ginkgo in parallel, so `task e2e`, `task e2e:focus`, and `task e2e:chaos` run one process.

Process 1 alone installs the operator and tears it down.
Every process connects to the cluster and runs the scenarios Ginkgo assigns to it, each in that process's own namespace for the scenario.
A scenario is one `Ordered` container, so its steps keep their order and their process, and a failed step skips the rest of that scenario while its cleanup still runs.

> [!important]
> A scenario MUST NOT depend on what another scenario left behind: every scenario starts from its own empty namespace.
> A scenario that blocks a resource which all processes share (the operator's single reconcile worker, a cluster-scoped object) MUST carry the `Serial` label so Ginkgo runs it on process 1 after the parallel scenarios.

A scenario that runs for two minutes or more SHOULD carry a `priority`, so that it starts early and not last.
Ginkgo starts the higher value first: `3` for about 230 seconds and up, `2` for 180 to 229 seconds, and `1` for 120 to 179 seconds.

### Writing scenarios

A scenario is a YAML file under `operators/<pack>/scenarios/`; its file name is the scenario name.
The parser rejects a file that breaks the rules (an unknown field, a step with zero or two kinds, a label Ginkgo cannot filter on, a Go step name that is not registered, a namespace that would exceed 63 characters), so a typo cannot drop a step silently.
`TestEmbeddedPacksParse` in `test/e2e` parses every embedded pack in `task test`, and it also requires every registered Go step to be used by at least one scenario.
[docs/scenarios.md](docs/scenarios.md) explains the step kinds and [docs/reference/scenario-schema.md](docs/reference/scenario-schema.md) lists every field.

A behavior pull request MUST ship its scenarios in the same change, so the kind job exercises them.
Run a new scenario locally with `task e2e:kind OPERATOR=<pack> FOCUS='<scenario>'` before opening the PR.

## Releasing

Tags `vX.Y.Z` are the release.
`release.yml` creates the GitHub release with notes generated from the merged pull requests; a tag with a hyphen is marked as a prerelease.
It also pushes the chart to `oci://ghcr.io/ydixken/charts/operator-testbench` (version `<tag>` without the `v`, appVersion `<tag>`), and the image `ghcr.io/ydixken/operator-testbench:<tag>` for `linux/amd64` and `linux/arm64`, without a `latest` tag: an image fits only the operator versions its packs pin.
Versions are plain SemVer, and pre-1.0 means the YAML schema can change.

## Commits and pull requests

- Conventional commits: `feat:`, `fix:`, `chore:`, `docs:`, `refactor:`, `test:`, `ci:`.
  Release notes are generated from pull request titles, so titles follow the same form.
- One logical change per commit.
  Every commit MUST be lint-clean on its own.
- `main` is protected: changes land via GitHub PRs, and `lint`, `test`, `docs`, `e2e-kind`, and `e2e-kind-cluster` MUST be green.
  Nobody pushes to `main` directly.
- Commits MUST NOT carry a `Co-Authored-By:` trailer naming an AI agent, and pull request text MUST NOT carry an agent attribution line.
  Claude session URLs MUST NOT appear in commits, trailers, or PR text (see [AGENTS.md](AGENTS.md#writing-conventions)).
