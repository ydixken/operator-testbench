# Deploying to a cluster

The Helm chart `operator-testbench` runs the scenarios against the operator a cluster already runs.
It installs nothing cluster-scoped except the scenario namespaces and one ClusterRole, and it never touches the operator or its CRDs.
A deploying repository (for example a GitOps repository with one directory per stage) pins the chart like any other release and reads the result from the release status.

## How a run works

The run is a Helm hook Job (`post-install,post-upgrade`).
Helm first creates the scenario namespaces and the RBAC, then starts the Job, and reports the release as failed when the Job fails.
Flux marks the HelmRelease `Ready=False`; Argo CD runs the hook as a PostSync hook and marks the sync failed.

The Job runs the image of the same release in [cluster mode](environments.md#testing-the-installed-operator): `TESTBENCH_OPERATOR_INSTALL=skip`, `TESTBENCH_NAMESPACES=provided`, and the context `in-cluster`.
Before the first scenario it checks that every Ready controller pod runs the operator version the pack pins, and fails otherwise.
The last Job and its log stay until the next run replaces them.

## Install

```sh
helm install testbench oci://ghcr.io/ydixken/charts/operator-testbench --version <version> \
  -n operator-testbench --create-namespace \
  --set operator.namespace=valkey-operator-system \
  --timeout 70m
```

Helm waits for the hook, and its `--timeout` (default 5 minutes) MUST be longer than a run.
`helm upgrade` runs the scenarios again.

## Values

| Value | Default | Meaning |
|-------|---------|---------|
| `operator.namespace` | required | The namespace the installed operator runs in. |
| `operator.version` | the pack's | `TESTBENCH_OPERATOR_VERSION`: expect a patch release of the operator version the pack pins. |
| `pack` | `valkey` | The pack under `operators/`. |
| `image.repository`, `image.tag` | `ghcr.io/ydixken/operator-testbench`, the chart's appVersion | The testbench image. Keep the tag empty, so chart and image come from one release. |
| `procs` | `1` | Ginkgo processes. Each process gets its own scenario namespaces. |
| `labelFilter` | `!chaos` | Ginkgo label filter. Chaos scenarios delete pods; run them only on purpose. |
| `timeoutMinutes` | `60` | Bounds the run: `ginkgo --timeout`, and the Job's `activeDeadlineSeconds` five minutes later. |
| `force` | `false` | Deletes what an aborted run left in the scenario namespaces. |
| `namespaceLabels` | `{}` | Added to every scenario namespace, for example Pod Security or a mesh opt-out. |
| `podAnnotations`, `podLabels`, `resources`, `nodeSelector`, `tolerations` | empty | The Job's pod. |
| `packs.<pack>.rules` | shipped per pack | The scenario role. |
| `externalPack.configMap`, `externalPack.files` | empty | Run a pack from a ConfigMap instead of the embedded one; see below. |

## Permissions

The Job's ServiceAccount gets:

- In each scenario namespace, through a RoleBinding to the chart's ClusterRole: the pack's `rules`, plus what the engine needs (`get` on its own namespace, pods and PVCs to find and clear leftovers, `pods/exec`, and events).
  A ClusterRole bound by a RoleBinding grants nothing outside that namespace.
- In `operator.namespace`: `get` and `list` on pods and `get` on `pods/log`, for the version check and the controller log tail.

Nothing else, and no cluster-wide rights.

## Version coupling

A testbench release fits only the operator version its pack pins, and the run fails when the cluster runs another.
When the deploying repository bumps the operator, it bumps the testbench chart in the same change, to the release whose pack pins that operator version.
Because the run polls for up to five minutes while old operator pods are replaced, the operator and the testbench can roll out in one change.

## Flux

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: operator-testbench
  namespace: operator-testbench
spec:
  type: oci
  url: oci://ghcr.io/ydixken/charts
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: testbench
  namespace: operator-testbench
spec:
  interval: 1h
  timeout: 70m  # longer than a run; Flux's default is 5m
  chart:
    spec:
      chart: operator-testbench
      version: <version>
      sourceRef: {kind: HelmRepository, name: operator-testbench}
  values:
    operator:
      namespace: valkey-operator-system
```

## Argo CD

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: testbench
  namespace: argocd
spec:
  project: default
  source:
    repoURL: ghcr.io/ydixken/charts
    chart: operator-testbench
    targetRevision: <version>
    helm:
      valuesObject:
        operator:
          namespace: valkey-operator-system
  destination:
    server: https://kubernetes.default.svc
    namespace: operator-testbench
  syncPolicy:
    syncOptions: [CreateNamespace=true]
```

Argo CD runs the Job as a PostSync hook on every sync.

## Your own pack

The image embeds its packs, and they stay the default.
A pack kept in your own repository replaces the embedded pack of the same name for one release, as a whole: it does not merge scenarios.

Put the pack's files into a ConfigMap in the release namespace, in the same sync as the chart.
A ConfigMap key cannot hold a `/`, so the key is the file's path inside the pack with `/` replaced by `__`:

```yaml
# kustomization.yaml
configMapGenerator:
  - name: my-pack
    namespace: operator-testbench
    options: {disableNameSuffixHash: true}
    files:
      - operator.yaml=valkey/operator.yaml
      - manifests__cluster.yaml=valkey/manifests/cluster.yaml
      - scenarios__smoke.yaml=valkey/scenarios/smoke.yaml
```

Then list the same paths in the chart's values:

```yaml
pack: valkey
externalPack:
  configMap: my-pack
  files:
    - operator.yaml
    - manifests/cluster.yaml
    - scenarios/smoke.yaml
```

The chart mounts each key at its path under `/packs/<pack>` and sets `TESTBENCH_PACK_DIR=/packs`.
The scenario namespaces come from the `scenarios/` entries, so the list cannot drift from the namespaces.
`helm template` fails when `operator.yaml` or every `scenarios/` entry is missing, or when a path is not lower case or is more than one directory deep.
Scenarios may use every step kind and the Go steps the image registers.
The pack's `operator.version` is the version the run expects, so a patch release of the operator needs only a change to your pack.

Check a pack before it reaches the cluster: `task pack:validate PACK_DIR=<dir>` in a checkout of this repository, or with the image alone:

```sh
docker run --rm -v "$PWD/packs:/packs:ro" -e TESTBENCH_PACK_DIR=/packs \
  --entrypoint /testbench/e2e.test ghcr.io/ydixken/operator-testbench:<version> -test.run '^TestPackDir$' -test.v
```

`<dir>` holds `<pack>/operator.yaml`, like `operators/` in this repository.

Whoever can change the ConfigMap decides what the Job does in the scenario namespaces: it runs `exec` and applies objects with the scenario role there.
Give the ConfigMap the same review as the chart values.

A ConfigMap or a listed key that is missing keeps the Job's pod in `ContainerCreating` until `activeDeadlineSeconds`.
Keep both in one sync, and validate the pack first.

## How the chart is tested

The workflow `e2e-kind-cluster.yml` does on every pull request what a deploying repository does: it installs valkey-operator from its upstream chart, then this chart from the working tree with an image built in the job, and runs the smoke scenarios.
A run from an external pack in a ConfigMap must show its only scenario, `external-smoke`, in the log.
A second run with `operator.version` set to a version the cluster does not run MUST fail.
A pull request merges only when the job is green.

## Recovery and pitfalls

- A run that was aborted leaves objects in the scenario namespaces, and the next run refuses them and lists them.
  Set `force: true` for one run, or delete them by hand.
- A mesh sidecar keeps the Job's pod running after the test container exits.
  Opt out with `podAnnotations` (for example `sidecar.istio.io/inject: "false"`) or `namespaceLabels` for the scenario namespaces.
- Two releases with the same `pack` cannot share a cluster: Helm refuses to create namespaces another release owns.
  Releases with different pack names can, for example the embedded `valkey` and your own pack under another name.
- `helm uninstall` removes the namespaces and the RBAC, but not the last hook Job; delete that by hand.
