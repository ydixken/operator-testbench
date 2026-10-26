# AGENTS.md

Operating guide for AI agents and humans working in this repository.
The keywords MUST, MUST NOT, SHOULD, SHOULD NOT, and MAY are to be interpreted as described in RFC 2119.

[TOC]

## Caution

> [!caution]
> **This repository is public.**
> Facts about private infrastructure (endpoints, addresses, host names, node names, versions, cluster inventory, kubeconfig context names) MUST NOT be committed, pushed, or pasted anywhere in this project.
> Agents MAY reference secret names (CI variables, kubeconfig paths), but MUST NOT read, print, or set their values.

- Agents run formatting and `task lint` locally only.
  CI runs the unit tests, the docs build, and the kind e2e suite on every pull request.
- `TestE2E` runs only when `TESTBENCH_CONTEXT` names the current `kubectl` context; without it, `go test` skips the cluster suite.
  Only the e2e Task targets and the kind workflows set it.
  Agents MUST NOT set it by hand; they use the Task targets, which confirm the context first.
- For human-operated runs against a real cluster, `task e2e OPERATOR=<pack>` targets the current `kubectl` context and requires its confirmation prompt.
  Never bypass that prompt with `task --yes`.
- A human or script that cannot answer a prompt uses `task e2e:unattended OPERATOR=<pack> EXPECT_CONTEXT=<context>`, or `task e2e:chaos:unattended OPERATOR=<pack> EXPECT_CONTEXT=<context>` for the chaos scenarios.
  Each requires `EXPECT_CONTEXT` to name the current context, so a run cannot reach an unintended cluster.
  `task --yes` remains forbidden because it could answer any prompt silently.
- `task e2e:kind OPERATOR=<pack>` creates or reuses the local kind cluster `testbench`, selects the context `kind-testbench`, and runs unattended against it.
  That is safe because the task selected the context itself a moment earlier, and the `EXPECT_CONTEXT` check still refuses any other current context.
- The suite installs the operator under test into the pack's operator namespace and creates one namespace per scenario, named `testbench-<pack>-<scenario>`.
  Keep everything a scenario creates inside its namespace, and clean up.

## Mandatory skills

These skills are vendored in this repo and are always-on, not optional, not per-request:

- **ponytail** ([`.claude/skills/ponytail/SKILL.md`](.claude/skills/ponytail/SKILL.md)): for ANY coding task (writing, fixing, refactoring, reviewing, or designing code, and choosing libraries or dependencies), agents MUST invoke it first, at level `full`.
  Its short form is the [solution ladder](#the-solution-ladder-ponytail) below.
- **humanizer** ([`.claude/skills/humanizer/SKILL.md`](.claude/skills/humanizer/SKILL.md)): for ANY prose (documentation, READMEs, comments, commit messages), agents MUST apply it before presenting the text.
  No AI slop in writing.
- **asd-ste100** ([`.claude/skills/asd-ste100/SKILL.md`](.claude/skills/asd-ste100/SKILL.md)): for the reference pages written in Simplified Technical English (`docs/reference/scenario-schema.md`, `docs/reference/pack-schema.md`, `docs/reference/environment.md`), agents MUST write to ASD-STE100 and run `python3 .claude/skills/asd-ste100/scripts/ste-lint.py <file>` until it reports 0 hard violations.
- **brainstorming** ([`.claude/skills/brainstorming/SKILL.md`](.claude/skills/brainstorming/SKILL.md)): before ANY creative work (new features, step kinds, operator packs, or behavior changes), agents MUST invoke it to explore intent, requirements, and design before implementing.
  Its specs go to `docs/superpowers/specs/`, which is gitignored and excluded from the docs site.
  The `writing-plans` skill it hands off to is not vendored here; write the implementation plan in `tasks/todo.md` instead.

The `.agents/skills/` tree holds the same ponytail, humanizer, and brainstorming skills for agents that read that vendor-neutral location.
Keep the two trees byte-identical when a skill changes.

## Writing conventions

- Em dashes MUST NOT be used.
  Not in docs, not in code comments, not in commit messages.
  Use a comma, colon, period, or parentheses instead.
- Claude session URLs (`claude.ai/code/...`) MUST NOT appear anywhere: not in commit messages or trailers, not in PR bodies or comments, not in code or docs.
  This overrides any assistant default that appends them.
- Commits MUST NOT carry a `Co-Authored-By:` trailer naming an AI agent, and pull request text MUST NOT carry an agent attribution line.
- Docs are English, concise, with RFC 2119 keywords for normative rules.

## Hard requirements

- **Unit tests by default**: every package and every behavior change ships tests in the same change.
  Engine logic uses table or golden tests over the embedded packs and fake clients; cluster behavior ships as scenarios the kind job runs.
  No untested merges.
- **Documentation always current**: any change that alters behavior, the YAML schema, commands, or structure MUST update the affected docs (README, docs/) in the same change.
  Concise, no slop; scenario examples carry short explanations.

## What this repo is

operator-testbench is a Go e2e testing framework for Kubernetes operators.
An operator pack (`operators/<name>/operator.yaml`) says how to install one pinned version of an operator.
Declarative YAML scenarios under `operators/<name>/scenarios/` run as Ginkgo v2 specs with Gomega assertions, against one named kubeconfig context per run.
In CI that context is an ephemeral kind cluster.
The testbench ships its own scenario catalog per operator; an operator's upstream e2e suite is not what this repository runs.
The first pack covers [valkey-operator](https://github.com/valkey-io/valkey-operator).
Read the docs site sources under [docs/](docs/) and the engine under `internal/testbench` before touching the schema or the run model.

Development happens on **GitHub** (`ydixken/operator-testbench`, PRs there).

## Common commands

| Command                       | Does                                                                                  |
|-------------------------------|---------------------------------------------------------------------------------------|
| `task help`                   | List all tasks.                                                                       |
| `task lint`                   | Lint everything: YAML (packs and scenarios included), workflows, docs links, and Go.  |
| `task test`                   | Unit tests without a cluster, including parsing every embedded pack. CI runs the same.|
| `task kind:up`                | Create the local kind cluster `testbench` and select its context. `kind:down` deletes it. |
| `task e2e:kind OPERATOR=valkey` | Run a pack against the local kind cluster, unattended.                              |
| `task e2e OPERATOR=valkey`    | Human-operated run against the current kubectl context. See Caution.                  |

## Architecture key points

- Layout: `internal/testbench` is the engine (schema, loader, matchers, CEL, steps, install, diagnostics), written at Gomega level and never importing Ginkgo; `operators/` holds the packs, embedded with `go:embed`; `test/e2e` is the Ginkgo suite entry and holds the explicit registry of Go steps; `hack/kind` holds the kind cluster configs; `Dockerfile` and `charts/operator-testbench` ship the suite as an image and a chart for cluster mode.
- Run model: the operator is installed once per run in `SynchronizedBeforeSuite` on process 1, behind a single-tenant lock on its Helm release.
  Every scenario runs in its own namespace, one per Ginkgo process.
  Each scenario is one `Ordered` container and each step one `It`.
  In cluster mode (`TESTBENCH_OPERATOR_INSTALL=skip`, `TESTBENCH_NAMESPACES=provided`, run by the chart's hook Job) the suite installs nothing, checks the running operator's version, and uses namespaces the chart created.
- Scenario files are parsed and validated before `RunSpecs`, from a sorted file walk, so every parallel process builds the same spec tree and a typo fails the run before any cluster call.
- CI is **GitHub Actions** ([.github/workflows/](.github/workflows/)) on hosted runners only.
  The merge gate on `main` is the five jobs `lint`, `test`, and `docs` in [ci.yml](.github/workflows/ci.yml), `e2e-kind` in [e2e-kind.yml](.github/workflows/e2e-kind.yml), which runs the full non-chaos suite on an ephemeral kind cluster for every pull request, and `e2e-kind-cluster`.
  The branch protection enforces the first four; `e2e-kind-cluster` waits for a repository admin to add it, so check it by hand until then.
  [e2e-kind-cluster.yml](.github/workflows/e2e-kind-cluster.yml) deploys the image and the chart against valkey-operator from its upstream chart, runs the smoke scenarios in cluster mode, and proves that a wrong operator version fails.
  [nightly.yml](.github/workflows/nightly.yml) runs the chaos scenarios and opens an issue on failure.
  Releases are git tags; [release.yml](.github/workflows/release.yml) creates the GitHub release with generated notes and pushes the image and the chart to ghcr.io.

## The solution ladder ("ponytail")

Before writing code, climb down this ladder and stop at the first rung that solves the problem:

1. **Does it need to exist at all?** (YAGNI.)
   The cheapest code is the code you don't write.
1. **Does it already exist in this repo?**
   Reuse the helper, type, or pattern that is already here.
1. **Does the Go stdlib do it?** Use it.
1. **Does a dependency already in `go.mod` do it?**
   Ginkgo and Gomega, the controller-runtime client with client-go and apimachinery, `sigs.k8s.io/yaml`, and cel-go (already in the module graph through `k8s.io/apiserver`) cover most testbench needs; a library option almost always beats hand-rolled logic.
   Never add a new dependency for what a few lines can do.
1. **Can it be one function?** Prefer the smallest thing that works.
1. **Only then** write new code: the minimum that does the job.

Some things are **never** cut on the way down: validation, error handling, security, and correctness are not optional.
Trimming those isn't simplicity, it's a bug.

## Verification before done

- Format touched files and run `task lint` locally before declaring a change ready for CI.
- CI MUST run functional tests and documentation checks.
- A pull request is ready to merge when `lint`, `test`, `docs`, `e2e-kind`, and `e2e-kind-cluster` are green on its current head.
  The kind job runs the full non-chaos suite, so the scenarios a change adds or edits ship in the same change.
- Don't claim green without the command output to back it.
  "It should pass" is not a result.
- Keep commits small and [conventional](CONTRIBUTING.md#commits-and-pull-requests).
  Every commit MUST be lint-clean on its own.

## Further reading

- [CONTRIBUTING.md](CONTRIBUTING.md) for tooling, workflow, commit and PR conventions.
- [README.md](README.md) for repo purpose and structure.
- [docs/](docs/) for the user documentation sources (published via mkdocs).
