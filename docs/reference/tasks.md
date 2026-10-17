# Task targets

`task --list-all` prints every target.
The cluster-touching targets read the current kubectl context, show it, and pass it to the suite as `TESTBENCH_CONTEXT` in the command itself.

| Target | Prompts | What it does |
|--------|---------|--------------|
| `task lint` | no | `yamllint --strict .`, the docs link check, `actionlint` when installed, and `golangci-lint run`. |
| `task test` | no | `go test ./... -skip '^TestE2E$' -race` with coverage. Parses every embedded pack. |
| `task kind:up [OPERATOR=valkey]` | no | Creates the kind cluster `testbench` from `hack/kind/<OPERATOR>.yaml` when it does not exist, selects the context `kind-testbench`, and preloads the pack's images. |
| `task kind:down` | no | Deletes the kind cluster `testbench`. |
| `task e2e OPERATOR=<pack>` | yes | The pack's scenarios without the `chaos` label, against the current context, one Ginkgo process. |
| `task e2e:focus OPERATOR=<pack> FOCUS=<regex>` | yes | One scenario or step by Ginkgo focus regex. `KEEP` defaults to `true`. |
| `task e2e:unattended OPERATOR=<pack> EXPECT_CONTEXT=<context>` | no | `e2e` without the prompt. `EXPECT_CONTEXT` must equal the current context. `LABELS` defaults to `!chaos`. |
| `task e2e:chaos OPERATOR=<pack>` | yes | Only the scenarios with the `chaos` label. |
| `task e2e:chaos:unattended OPERATOR=<pack> EXPECT_CONTEXT=<context>` | no | `e2e:chaos` without the prompt. |
| `task e2e:kind OPERATOR=<pack>` | no | `kind:up`, then the pack against `kind-testbench` through the ginkgo CLI with `PROCS` processes (default `2`). `LABELS` defaults to `!chaos`. |

Variables every e2e target accepts: `FOCUS` (Ginkgo focus regex), `TIMEOUT` (Go duration for the whole run, default `1h`), `VERSION`, `INSTALL` (`helm` or `skip`), `KEEP`, `FORCE`.
`task e2e:kind` and `task e2e:unattended` also accept `LABELS`.

`task --yes` MUST NOT be used.
It would answer every prompt, including one for the wrong cluster.
