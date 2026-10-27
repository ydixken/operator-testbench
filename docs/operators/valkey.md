# Valkey

The pack for [valkey-operator](https://github.com/valkey-io/valkey-operator), pinned to operator `v0.8.0` and chart `0.8.0`.
The operator's v1alpha1 API changed in nearly every 0.x minor, so the pack, the chart version and the scenarios move together in one pull request; `TESTBENCH_OPERATOR_VERSION` is a patch-level override.

## What the pack installs

- The two CRDs, `ValkeyCluster` and `ValkeyNode`, applied server-side from the operator repository at the pinned tag, because Helm never upgrades CRDs.
- The `valkey-operator` chart from `https://valkey.io/valkey-helm` into the namespace `testbench-valkey-system`, as the release `testbench-valkey-operator`, with `image.tag` set to the operator version.
- Nothing else: the operator needs neither cert-manager nor the Prometheus Operator.

## Scenarios

| Scenario | Labels | What it proves | Needs |
|----------|--------|----------------|-------|
| `cluster-ready` | `smoke` | A 3x1 `ValkeyCluster` converges (state, reason, shard counters, the four conditions at the applied generation), six `ValkeyNodes` are ready with one primary per shard, the owned Service, ConfigMap, Secrets and PodDisruptionBudget have the promised shape, `CLUSTER INFO` reports `cluster_state:ok`, and deleting the CR removes everything it owns. | About 600m CPU and 1.5Gi memory requested for six pods. |
| `data-path` | `smoke` | 500 keys written and read back through `valkey-cli -c` across the shards, the headless Service FQDN answers inside the namespace, `spec.config` reached the server, every pod runs the pinned exporter sidecar, and the exporter reports `redis_up 1`. | The same 3x1 cluster, in its own namespace. |
| `scale-replicas` | `scale` | A 1x0 cluster is Ready, then a re-applied 1x1 adds a replica that reports `role:slave` with its link up while node 0 stays the primary. | One or two pods. |
| `scale-shards` | `scale` | 2x1 to 3x1 and back: slot ownership moves to three primaries and back to two, the drained shard's nodes go away, and 500 seeded keys survive both migrations. | Valkey 9.0 or later for atomic slot migration (the manifests pin 9.0.0); up to 25 minutes. |
| `persistence-restart` | `persistence` | A 3x1 cluster with 1Gi volumes and appendonly survives every pod being deleted at once and keeps a canary key; its PVCs are retained when the CR goes, and a 1x1 cluster with `reclaimPolicy: Delete` loses them with the CR. The last step deletes the retained PVCs. | A default StorageClass with a dynamic RWO provisioner (kind's standard one). |
| `failover` | `chaos` | The live primary of shard 0, found through `INFO replication`, is deleted; its replica reports `role:master` within a minute, the cluster recovers with one primary per shard, the recreated pod rejoins as a replica, and 50 keys written before the failover read back. | Nightly only; Valkey 9.0's graceful handoff is best-effort. |

Every scenario runs in its own namespace, `testbench-valkey-<scenario>`, and every `exec` runs `valkey-cli` in the `server` container after `unset VALKEYCLI_AUTH REDISCLI_AUTH`: that variable carries the operator's internal password, and the default user has none.

## The Go step

`valkey.shardRoles` takes the args `cluster` and `shard` and returns `primary` and `replica`, the pod names of the shard's live primary and of a replica whose link is up, read from `INFO replication` in every pod of the shard.
It exists because `ValkeyNode` `status.role` lags a failover and the `valkey.io/node-index` labels never move.
The failover scenario addresses those pods as `{{ .Vars.primary }}` and `{{ .Vars.replica }}`.

## Facts the scenarios rely on

- Ready means `status.state == Ready`, `reason == ClusterHealthy`, `readyShards == shards`, and the conditions `Ready=True`, `Progressing=False/ReconcileComplete`, `ClusterFormed=True`, `SlotsAssigned=True`, each with `observedGeneration == metadata.generation`.
  Pod readiness alone means nothing: the readiness probe only answers `PING`.
- For a cluster named `demo`, the operator creates the headless Service `valkey-demo` on port 6379, the ConfigMap `valkey-demo`, the Secrets `internal-demo-system-passwords` and `internal-demo-acl`, the PodDisruptionBudget `valkey-demo`, and one `ValkeyNode` per shard and node, `demo-<shard>-<node>`, each backed by a StatefulSet whose pod is `valkey-demo-<shard>-<node>-0`.
- Pod IPs are not routable from the test runner, so every check goes through `kubectl exec`.
  The exporter image has no shell, so its metrics are read from the `server` container over the pod's loopback with bash's `/dev/tcp`.
- PersistentVolumeClaims are retained by default and keep a stale `nodes.conf`; the persistence scenario deletes them in its last step, so a provided namespace starts empty on the next run.

## Not covered yet

TLS with cert-manager, users and live ACL updates, custom pod metadata and ServiceAccounts (v0.8), a rolling image upgrade, read-only root filesystems under a restricted Pod Security Standard, topology spread, events, admission rejections, and a continuous writer during a failover.
Each needs either a cluster-wide dependency or a step kind the engine does not have yet.
