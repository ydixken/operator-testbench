# Operator packs

A pack tells the engine how to install one pinned version of an operator, and holds that operator's scenarios.
The fields are listed in the [pack schema](reference/pack-schema.md).

## Layout

```text
operators/valkey/
  operator.yaml          how to install, which version, where
  manifests/*.yaml       what the scenarios apply
  scenarios/*.yaml       one scenario per file
```

The directory name is the pack name.
It is the value of `TESTBENCH_OPERATOR` and of the `OPERATOR` variable of the Task targets, and it becomes a Ginkgo label on every scenario.
A pack name is a DNS label of at most 16 characters, so that `testbench-<pack>-<scenario>-<N>` fits a namespace name.

## operator.yaml

```yaml
apiVersion: testbench.dev/v1alpha1
kind: OperatorPack
operator:
  # renovate: datasource=github-releases depName=valkey-io/valkey-operator
  version: v0.8.0
  namespace: testbench-valkey-system
  selector: app.kubernetes.io/name=valkey-operator,control-plane=controller-manager
install:
  crds:
    - https://raw.githubusercontent.com/valkey-io/valkey-operator/{{ .Version }}/config/crd/bases/valkey.io_valkeyclusters.yaml
  helm:
    repo: https://valkey.io/valkey-helm
    chart: valkey-operator
    # renovate: datasource=helm depName=valkey-operator registryUrl=https://valkey.io/valkey-helm
    version: 0.8.0
    release: testbench-valkey-operator
    set:
      image.tag: "{{ .Version }}"
    watchNamespacesValue: manager.watchNamespaces
```

`operator.version` is the default operator version.
`TESTBENCH_OPERATOR_VERSION` overrides it, and `{{ .Version }}` in the CRD URLs and the Helm values follows the override.
Treat it as a patch-level knob: an operator whose minor changes its API needs the chart version, the CRDs, and the scenarios bumped together, in one pull request.

`operator.namespace` is where the engine installs.
The engine creates it with its own label, so it can tell at teardown whether the namespace is its own to delete.
Pick a name that no production install uses.

`operator.selector` matches the controller pods in that namespace.
The engine waits for one of them to be Ready after the install, and reads their logs when a step fails.
It also reads the operator version from the image tag of their controller container: `operator.container` names it, and a pod with one container needs no name.

## CRDs before the chart

Helm installs the CRDs a chart carries, and never upgrades them.
A pack therefore lists its CRD manifests under `install.crds`, and the engine applies them server-side, before the chart and on every run.
The chart is installed with `--skip-crds`.
The CRDs stay on the cluster after the run: deleting a CRD deletes every object of that kind, cluster-wide.

Before it applies a manifest, the engine reads the field managers of the CRDs in it.
A CRD that exists without the engine's field manager (`operator-testbench`) belongs to another install of the operator, and replacing it would change that operator's API.
The run then fails and names the CRD.
`TESTBENCH_FORCE=true` replaces it anyway and logs which CRD it replaced.

## Watched namespaces

Most operators watch every namespace by default.
The installed operator would then reconcile objects that belong to someone else on the cluster.
When the chart can restrict it, name that list value in `install.helm.watchNamespacesValue`.
The engine sets it to every scenario namespace of the run, for every Ginkgo process, for example `--set manager.watchNamespaces={testbench-valkey-cluster-ready,...}`.
Leave the field out for a chart without such a value.

## The single-tenant lock

The engine installs the chart as `install.helm.release` in `operator.namespace`, and `helm status` on that release is the lock.
When the release exists, another run is active on that cluster, or a crashed run left it behind, and the suite refuses to start.
`TESTBENCH_FORCE=true` takes the release over, and also replaces CRDs that another install owns.

## Renovate

The `# renovate:` comments above the two versions let one Renovate custom manager serve every pack.
Bumps of the operator and the chart are labelled `dependencies/major` and never automerged, because a bump has to be read against the scenarios.

## Adding a pack

1. Create `operators/<name>/operator.yaml` and run `task test`; the parser names what is missing.
2. Add a manifest and a first scenario, and run `task e2e:kind OPERATOR=<name>` against a kind cluster; `hack/kind/<name>.yaml` is the cluster shape that task uses, so add one.
3. Document the pack under `docs/operators/<name>.md` and add it to the navigation in `mkdocs.yml`.
4. Add the pack to the kind workflow's matrix when it should run on every pull request.
