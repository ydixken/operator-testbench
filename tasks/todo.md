# Plans

## Cluster mode

Design: `docs/superpowers/specs/2026-10-23-cluster-mode-design.md` (local only).
One pull request each, on local branches; nothing is pushed until the author has reviewed it.

### 1. `fix(testbench)`: foreign CRDs and watch namespaces

- [x] `ApplyCRDs` lists the field managers of each CRD with `kubectl get -f <url> --ignore-not-found -o custom-columns=...`.
  A CRD that exists without the engine's field manager fails the run and is named; `TESTBENCH_FORCE=true` replaces it and logs that.
- [x] Pack field `install.helm.watchNamespacesValue` (valkey: `manager.watchNamespaces`), validated: no `=`, `,` or whitespace, and not also a key of `install.helm.set`.
- [x] `Pack.Namespaces(procs)` lists every scenario namespace of a run; `HelmInstall` sets the watch value to that list, with `procs` from the suite's Ginkgo configuration.
- [x] Unit tests: command lines, owned, foreign, forced and absent CRDs, the watch value, schema rejects.
- [x] Docs: `docs/packs.md`, `docs/reference/pack-schema.md` (STE, `ste-lint.py` at 0 hard violations), `docs/environments.md` (`TESTBENCH_FORCE`, the "never touched" sentence), the pack and suite comments.
- [x] `task lint` and `task test` green locally (2026-10-26).

### 2. `feat(testbench)`: cluster mode

- [x] `TESTBENCH_OPERATOR_NAMESPACE` and `TESTBENCH_NAMESPACES=managed|provided` in `ParseEnv`, rejected values quoted.
- [x] Optional pack field `operator.container`; version check inside `WaitReady`, in every mode: tag parsing (digest, registry port, `v` prefix), polling until all Ready pods run the expected tag, at once on no tag or a missing container.
- [x] `EnsureNamespace` with `provided`: namespace MUST exist and be empty of pods, PVCs and the scenario's kinds; `TESTBENCH_FORCE=true` clears them first.
- [x] `Cleanup` keeps a provided namespace; teardown skips `DeleteLabelledNamespaces`.
- [x] `persistence-restart.yaml` deletes its retained PVCs in a final step.
- [x] Task variables `OPERATOR_NAMESPACE` and `NAMESPACES` on every e2e target.
- [x] Unit tests and docs (`environments.md`, `reference/environment.md` in STE, `reference/pack-schema.md`, `operators/valkey.md`).
- [x] `task lint` and `task test` green locally (2026-10-26).
- [ ] Cluster test by the maintainer before merge.

### 3. `feat`: container image

- [x] Multi-stage `Dockerfile`: `ginkgo build`, ginkgo CLI, pinned static kubectl, distroless `static-debian12:nonroot`, Renovate annotations.
- [x] `task image:build`; `release.yml` pushes `ghcr.io/ydixken/operator-testbench:<tag>` for amd64 and arm64, no `latest`.
- [x] `task lint` and `task test` green locally; both platforms cross-compile statically (2026-10-26). No local Docker: the first image build runs in CI.

### 4. `feat(chart)`: Helm chart

- [ ] `charts/operator-testbench`: namespaces, scenario ClusterRole and RoleBindings, operator-pod Role, ServiceAccount, hook Job.
- [ ] `task chart:generate` writes `files/scenarios.yaml` from the embedded packs; a unit test checks it is current.
- [ ] `task chart:lint` (`helm lint --strict`, `helm template` through yamllint) runs from `task lint`.
- [ ] `release.yml` pushes the chart to `oci://ghcr.io/ydixken/charts/operator-testbench`.

### 5. `ci` and `docs`: cluster-mode kind job

- [ ] `e2e-kind-cluster`: upstream valkey-operator chart, image via `kind load`, chart from the working tree, `labelFilter: smoke`; a second install with a wrong version MUST fail.
- [ ] `docs/deploying.md` in the nav; `docs/reference/tasks.md`; `README.md` and `AGENTS.md` for `charts/`, `Dockerfile` and the run model.

### Open questions

- Image and chart under `ghcr.io/ydixken/...`, chart under `charts/`?
- Does `e2e-kind-cluster` join the merge gate?
- Chart version equal to the testbench tag, or independent?

## Bootstrap (done)

Design: the brainstorming spec in `docs/superpowers/specs/2026-10-18-operator-testbench-design.md` (local only).

### Scaffold (direct push to main, before protection)

- [x] Toolchain: go 1.27.2, kind 0.33.0, golangci-lint 2.15.0, yamllint 1.38.0, actionlint 1.7.12 installed and verified.
- [x] `chore: scaffold the repository`: LICENSE, .gitignore, AUTHORS.md, README.md, CLAUDE.md, AGENTS.md, CONTRIBUTING.md, tasks/.
- [x] `chore: add the Taskfile, lint configuration and kind helpers`.
- [x] `chore: vendor the agent skills`.
- [x] `docs: mkdocs site skeleton`.
- [x] `ci: lint, test, docs and release workflows plus the context gate`.
- [x] Private-facts grep returns nothing before the first push.
- [x] GitHub repository created and configured; CI green on main; branch protection with lint, test, docs.
  Recreated once on 2026-10-18 to purge co-author trailers from a closed pull request's history.

### Engine (one PR each)

- [x] `feat(testbench): CEL environment`.
- [x] `feat(testbench): scenario and pack schema with strict validation`.
- [x] `feat(testbench): pack loader and environment knobs`.
- [x] `feat(testbench): subset matching, report formatting and redaction`.
- [x] `feat(testbench): run with apply, assert, delete and go steps`.
- [x] `feat(testbench): exec step over kubectl`.
- [x] `feat(testbench): helm and kubectl operator install`.
- [x] `feat(testbench): failure diagnostics`.

### Valkey pack and the suite

- [x] `feat(valkey): operator pack, cluster-ready scenario and the Ginkgo suite`; `task e2e:kind OPERATOR=valkey` green locally (19 of 19 specs, 67 s, 2026-10-18).
- [x] `ci: run the valkey pack on kind for every pull request`; protection extended with e2e-kind (2026-10-18).
- [x] `feat(valkey): data-path, scale-replicas, scale-shards and persistence-restart scenarios`; 67 of 67 specs green locally in 3 min 52 s (2026-10-18).
- [x] `feat(valkey): shard role Go step and the failover chaos scenario`; `task e2e:kind OPERATOR=valkey LABELS=chaos` green locally (10 of 10 specs, 79 s, 2026-10-18).
- [x] `ci: nightly chaos run on kind that opens an issue on failure`; dispatched run https://github.com/ydixken/operator-testbench/actions/runs/39081622022 passed on 54d0991 (2026-10-18).
- [x] `docs: concepts, scenarios, packs, environments, reference and troubleshooting`; STE pages lint clean.

#### Verification

- [x] Full non-chaos suite with `--procs=2` under 50 minutes on ubuntu-latest: about 5 minutes end to end on the merge of #3 (2026-10-18).
- [x] From a clean workstation: `task e2e:kind OPERATOR=valkey` (67 of 67), `task e2e:kind OPERATOR=valkey LABELS=chaos` (10 of 10), no `testbench` namespace or release left, `task kind:down` (2026-10-18).

Mark a remote gate done only with its run URL and the exact SHA.
