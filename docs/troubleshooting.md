# Troubleshooting

Symptoms mapped to causes and fixes.

| Symptom | Cause | Fix |
|---------|-------|-----|
| `TestE2E` is skipped and the run is green with no specs. | `TESTBENCH_CONTEXT` is unset. | Use a Task target. The kind workflows refuse an empty context on purpose, and `--fail-on-empty` turns a run of nothing red. |
| `TESTBENCH_CONTEXT does not name the current kubectl context`. | The current context changed between confirming and running, or the variable was set by hand. | Run the Task target again; it reads the current context itself. |
| `helm release exists: testbench-<pack>-operator in testbench-<pack>-system`. | Another run is active on that cluster, or a crashed run left the release behind. | Wait for the other run, or rerun with `FORCE=true`. |
| `no Ready pod matches <selector> in namespace <ns>`. | The operator did not start, or with `INSTALL=skip` it is not installed where the pack expects it. | Read the controller log entry on the failed suite setup, or install the operator first. |
| `the server could not find the requested resource` on an assert or apply. | The CRD is missing or serves another version than the scenario uses. | Check the pack's `install.crds` and the operator version; `TESTBENCH_OPERATOR_VERSION` moves both. |
| `not satisfied after 10m0s: object lacks the expected subset`. | The operator did not reach the expected state in time. | Read the `namespace` and `controller logs` report entries attached to the failed step: pods, events in time order, and the controller's last lines. |
| `nothing matches Pod valkey.io/cluster=x; a delete that finds nothing is a mistake in the scenario`. | A selector with a typo. | Fix the selector; an empty delete is never a pass. |
| `go step valkey.shardRoles: ... no pod of the shard reports role:master yet`. | The shard has no primary yet, or the pods are not Running. | Give the step more time, or assert readiness before it. |
| `Error response from daemon: toomanyrequests` while preloading images. | Docker Hub's anonymous pull limit on a shared runner address. | Rerun later; the preload pulls each image once, which is the mitigation. |
| A kept namespace will not delete. | Objects with finalizers, and the operator that owns them is gone. | Run the pack once more with `KEEP=false`, or install the operator and delete the namespace afterwards. |
| `label "x:" has nothing after ':'` or `contains one of &|!,()/`. | A label Ginkgo cannot filter on. | Rename the label. |
| `match.status.conditions is an empty list, which matches anything`. | An empty value in `match`, which the subset matcher would treat as a wildcard. | Use `cel` for the check, or name a value. |

## Reading a failed run

A failed step prints the last reason it was not satisfied, then two report entries: the namespace's pods with their container states and the newest events in time order, and the redacted tail of the controller logs.
Addresses, URLs, and values after a password, secret or token key are redacted, because CI logs are public.

On kind in CI, the `e2e-kind-reports` artifact holds `report.json`, `junit.xml`, and, when the run failed, the kind logs of every node under `kind-logs/`.
Locally, `task e2e:kind` writes the reports to `reports/`.
