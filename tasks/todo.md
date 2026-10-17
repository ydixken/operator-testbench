# Bootstrap plan

Design: the brainstorming spec in `docs/superpowers/specs/2026-10-18-operator-testbench-design.md` (local only).

## Scaffold (direct push to main, before protection)

- [x] Toolchain: go 1.27.2, kind 0.33.0, golangci-lint 2.15.0, yamllint 1.38.0, actionlint 1.7.12 installed and verified.
- [x] `chore: scaffold the repository`: LICENSE, .gitignore, AUTHORS.md, README.md, CLAUDE.md, AGENTS.md, CONTRIBUTING.md, tasks/.
- [x] `chore: add the Taskfile, lint configuration and kind helpers`.
- [x] `chore: vendor the agent skills`.
- [x] `docs: mkdocs site skeleton`.
- [x] `ci: lint, test, docs and release workflows plus the context gate`.
- [x] Private-facts grep returns nothing before the first push.
- [ ] GitHub repository created and configured; CI green on main; branch protection with lint, test, docs.

## Engine (one PR each)

- [ ] `feat(testbench): CEL environment`.
- [ ] `feat(testbench): scenario and pack schema with strict validation`.
- [ ] `feat(testbench): pack loader and environment knobs`.
- [ ] `feat(testbench): subset matching, report formatting and redaction`.
- [ ] `feat(testbench): run with apply, assert, delete and go steps`.
- [ ] `feat(testbench): exec step over kubectl`.
- [ ] `feat(testbench): helm and kubectl operator install`.
- [ ] `feat(testbench): failure diagnostics`.

## Valkey pack and the suite

- [ ] `feat(valkey): operator pack, cluster-ready scenario and the Ginkgo suite`; `task e2e:kind OPERATOR=valkey` green locally.
- [ ] `ci: run the valkey pack on kind for every pull request`; protection extended with e2e-kind.
- [ ] `feat(valkey): data-path, scale-replicas, scale-shards and persistence-restart scenarios`.
- [ ] `feat(valkey): shard role Go step and the failover chaos scenario`; `task e2e:kind OPERATOR=valkey LABELS=chaos` green locally.
- [ ] `ci: nightly chaos run on kind that opens an issue on failure`; one dispatched run proves it.
- [ ] `docs: concepts, scenarios, packs, environments, reference and troubleshooting`; STE pages lint clean.

### Verification

- [ ] Full non-chaos suite with `--procs=2` under 50 minutes on ubuntu-latest (record the measured time here).
- [ ] From a clean workstation: `task kind:up`, `task e2e:kind OPERATOR=valkey`, `task e2e:kind OPERATOR=valkey LABELS=chaos`, no `testbench` namespace or release left, `task kind:down`.

Mark a remote gate done only with its run URL and the exact SHA.
