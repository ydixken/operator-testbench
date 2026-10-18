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
- [x] GitHub repository created and configured; CI green on main; branch protection with lint, test, docs.
  Recreated once on 2026-10-18 to purge co-author trailers from a closed pull request's history.

## Engine (one PR each)

- [x] `feat(testbench): CEL environment`.
- [x] `feat(testbench): scenario and pack schema with strict validation`.
- [x] `feat(testbench): pack loader and environment knobs`.
- [x] `feat(testbench): subset matching, report formatting and redaction`.
- [x] `feat(testbench): run with apply, assert, delete and go steps`.
- [x] `feat(testbench): exec step over kubectl`.
- [x] `feat(testbench): helm and kubectl operator install`.
- [x] `feat(testbench): failure diagnostics`.

## Valkey pack and the suite

- [x] `feat(valkey): operator pack, cluster-ready scenario and the Ginkgo suite`; `task e2e:kind OPERATOR=valkey` green locally (19 of 19 specs, 67 s, 2026-10-18).
- [x] `ci: run the valkey pack on kind for every pull request`; protection extended with e2e-kind (2026-10-18).
- [x] `feat(valkey): data-path, scale-replicas, scale-shards and persistence-restart scenarios`; 67 of 67 specs green locally in 3 min 52 s (2026-10-18).
- [x] `feat(valkey): shard role Go step and the failover chaos scenario`; `task e2e:kind OPERATOR=valkey LABELS=chaos` green locally (10 of 10 specs, 79 s, 2026-10-18).
- [x] `ci: nightly chaos run on kind that opens an issue on failure`; dispatched run https://github.com/ydixken/operator-testbench/actions/runs/39081622022 passed on 54d0991 (2026-10-18).
- [x] `docs: concepts, scenarios, packs, environments, reference and troubleshooting`; STE pages lint clean.

### Verification

- [x] Full non-chaos suite with `--procs=2` under 50 minutes on ubuntu-latest: about 5 minutes end to end on the merge of #3 (2026-10-18).
- [x] From a clean workstation: `task e2e:kind OPERATOR=valkey` (67 of 67), `task e2e:kind OPERATOR=valkey LABELS=chaos` (10 of 10), no `testbench` namespace or release left, `task kind:down` (2026-10-18).

Mark a remote gate done only with its run URL and the exact SHA.
