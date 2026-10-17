# Valkey

The pack for [valkey-operator](https://github.com/valkey-io/valkey-operator), pinned to operator `v0.7.1` and chart `0.7.0`.
The operator's v1alpha1 API changed in nearly every 0.x minor, so the pack, the chart version and the scenarios move together in one pull request; `TESTBENCH_OPERATOR_VERSION` is a patch-level override.

## What the pack installs

- The two CRDs, `ValkeyCluster` and `ValkeyNode`, applied server-side from the operator repository at the pinned tag, because Helm never upgrades CRDs.
- The `valkey-operator` chart from `https://valkey.io/valkey-helm` into the namespace `testbench-valkey-system`, as the release `testbench-valkey-operator`, with `image.tag` set to the operator version.
- Nothing else: the operator needs neither cert-manager nor the Prometheus Operator.

## Scenarios

| Scenario | Labels | What it proves | Needs |
|----------|--------|----------------|-------|
| `cluster-ready` | `smoke` | A 3x1 `ValkeyCluster` converges (state, reason, shard counters, the four conditions at the applied generation), six `ValkeyNodes` are ready with one primary per shard, the owned Service, ConfigMap, Secrets and PodDisruptionBudget have the promised shape, `CLUSTER INFO` reports `cluster_state:ok`, and deleting the CR removes everything it owns. | Pulls `valkey/valkey:9.0.0` and `oliver006/redis_exporter:v1.88.0`. About 600m CPU and 1.5Gi memory requested for six pods. |

Every scenario runs in its own namespace, `testbench-valkey-<scenario>`, and every `exec` runs `valkey-cli` in the `server` container after `unset VALKEYCLI_AUTH REDISCLI_AUTH`: that variable carries the operator's internal password, and the default user has none.

## Facts the scenarios rely on

- Ready means `status.state == Ready`, `reason == ClusterHealthy`, `readyShards == shards`, and the conditions `Ready=True`, `Progressing=False/ReconcileComplete`, `ClusterFormed=True`, `SlotsAssigned=True`, each with `observedGeneration == metadata.generation`.
  Pod readiness alone means nothing: the readiness probe only answers `PING`.
- For a cluster named `demo`, the operator creates the headless Service `valkey-demo` on port 6379, the ConfigMap `valkey-demo`, the Secrets `internal-demo-system-passwords` and `internal-demo-acl`, the PodDisruptionBudget `valkey-demo`, and one `ValkeyNode` per shard and node, `demo-<shard>-<node>`, each backed by a StatefulSet whose pod is `valkey-demo-<shard>-<node>-0`.
- Pod IPs are not routable from the test runner, so every check goes through `kubectl exec`.
