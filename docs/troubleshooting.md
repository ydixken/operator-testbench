# Troubleshooting

Symptoms mapped to causes and fixes.

| Symptom | Cause | Fix |
|---------|-------|-----|
| `TestE2E` is skipped and the run is green with no specs. | `TESTBENCH_CONTEXT` is unset. | Use a Task target. The kind workflows refuse an empty context on purpose, and `--fail-on-empty` turns a run of nothing red. |
| `TESTBENCH_CONTEXT does not name the current kubectl context`. | The current context changed between confirming and running, or the variable was set by hand. | Run the Task target again; it reads the current context itself. |
| `helm release exists: testbench-<pack>-operator in testbench-<pack>-system`. | Another run is active on that cluster, or a crashed run left the release behind. | Wait for the other run, or rerun with `FORCE=true`. |
| `CRD <name> exists and this suite did not apply it`. | Another install of the operator owns the CRD, and replacing it would change that operator's API. | Test that operator with `INSTALL=skip`, or rerun with `FORCE=true` to replace the CRD. |
| `no Ready pod matches <selector> in namespace <ns>`. | The operator did not start, or with `INSTALL=skip` it is not installed where the pack expects it. | Read the controller log entry on the failed suite setup, or install the operator first. |
| `the testbench expects operator version <v>, but <pod> runs <tag>`. | The cluster runs another operator version than the pack pins, or a rollout is not finished after five minutes. | Use the testbench release whose pack pins the running version; `VERSION` covers a patch release. |
| `pod <name> runs image <image> without a tag`. | The operator is deployed by digest only. | Deploy it by tag, or by tag and digest. |
| `namespace <ns> does not exist; with TESTBENCH_NAMESPACES=provided it must be created before the run`. | The namespaces were not created, or the chart and the image come from different testbench releases. | Create the namespaces, or use the chart and image of one release. |
| The Job's pod stays in `ContainerCreating` with `configmap ... not found` or a missing key. | A volume in `extraVolumes` names a ConfigMap or a key that does not exist yet. | Create the volume source in the same sync as the chart, and keep its keys and the `items` in step. |
| `TESTBENCH_PACK_DIR="packs" must be an absolute path`. | A relative path. | Pass an absolute path; `task pack:validate` makes one for you. |
| `namespace <ns> is not empty`. | An aborted run left objects in a provided namespace. | Rerun with `FORCE=true`; it deletes the listed objects in that namespace only. |
| `the server could not find the requested resource` on an assert or apply. | The CRD is missing or serves another version than the scenario uses. | Check the pack's `install.crds` and the operator version; `TESTBENCH_OPERATOR_VERSION` moves both. |
| `not satisfied after 10m0s: object lacks the expected subset`. | The operator did not reach the expected state in time. | Read the `namespace` and `controller logs` report entries attached to the failed step: pods, events in time order, and the controller's last lines. |
| `nothing matches Pod valkey.io/cluster=x; a delete that finds nothing is a mistake in the scenario`. | A selector with a typo. | Fix the selector; an empty delete is never a pass. |
| `go step valkey.shardRoles: ... no pod of the shard reports role:master yet`. | The shard has no primary yet, or the pods are not Running. | Give the step more time, or assert readiness before it. |
| `unknown flag: --platform` while preloading images. | Docker older than 28, which cannot save one platform of an image. The preload needs that for Docker's containerd image store. | Update Docker. |
| `Error response from daemon: toomanyrequests` while preloading images. | Docker Hub's anonymous pull limit on a shared runner address. | Rerun later; the preload pulls each image once, which is the mitigation. |
| A kept namespace will not delete. | Objects with finalizers, and the operator that owns them is gone. | Run the pack once more with `KEEP=false`, or install the operator and delete the namespace afterwards. |
| `label "x:" has nothing after ':'` or `contains one of &|!,()/`. | A label Ginkgo cannot filter on. | Rename the label. |
| `match.status.conditions is an empty list, which matches anything`. | An empty value in `match`, which the subset matcher would treat as a wildcard. | Use `cel` for the check, or name a value. |

## Reading a failed run

A failed step prints the last reason it was not satisfied, then two report entries: the namespace's pods with their container states and the newest events in time order, and the redacted tail of the controller logs.
Addresses, URLs, and values after a password, secret or token key are redacted, because CI logs are public.

On kind in CI, the `e2e-kind-reports` artifact holds `report.json`, `junit.xml`, and, when the run failed, the kind logs of every node under `kind-logs/`.
Locally, `task e2e:kind` writes the reports to `reports/`.
