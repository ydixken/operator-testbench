# Writing scenarios

A scenario is a YAML file under `operators/<pack>/scenarios/`.
This page explains how to write one; the [scenario schema](reference/scenario-schema.md) lists every field.

## A first scenario

```yaml
apiVersion: testbench.dev/v1alpha1
kind: Scenario
labels: [smoke]
priority: 1
steps:
  - name: apply a 3x1 cluster
    timeout: 1m
    apply: {file: manifests/cluster-3x1.yaml}

  - name: the cluster reports Ready
    timeout: 10m
    assert:
      object: {apiVersion: valkey.io/v1alpha1, kind: ValkeyCluster, name: demo}
      match:
        status: {state: Ready, readyShards: 3}
      cel:
        - self.status.conditions.exists(c, c.type == 'Ready' && c.status == 'True')

  - name: valkey answers
    timeout: 2m
    exec:
      selector: valkey.io/cluster=demo
      container: server
      command: [sh, -c, 'valkey-cli CLUSTER INFO']
      expect: {contains: ['cluster_state:ok']}
```

The file name is the scenario name, so this file would be `scenarios/cluster-ready.yaml` and run in the namespace `testbench-valkey-cluster-ready`.
Each step becomes one Ginkgo spec named after its position and `name`.
A failed step skips the rest of the scenario; the cleanup still runs.

## Steps

### apply

`apply.file` is a manifest in the pack directory, usually under `manifests/`.
It MAY hold several documents.
Every object needs `apiVersion`, `kind`, and `metadata.name`.
A namespaced object without a namespace lands in the scenario namespace; one that names another namespace is refused.
The engine applies with server-side apply under the field manager `operator-testbench`, and remembers what it applied so it can delete it again at the end.

To change a spec, apply a second manifest with the whole object and the changed fields.
The `scale-shards` scenario of the Valkey pack does that with `scale-2x1.yaml` and `scale-3x1.yaml`, which differ in one line.

### assert

An `assert` names either one `object` or a `list`.

For an object:

- `match` is a subset the live object must contain.
  Every key you name must be present with that value; keys you do not name are ignored; a list is an ordered prefix.
  Numbers compare like the API server stores them, so `readyShards: 3` matches the integer the operator wrote.
  An empty string, an empty list or map, and `null` are refused, because they would match anything.
- `cel` is a list of expressions over `self`, the object.
  Every expression must be true at the same time.
  The dialect is the one CRD validation rules use, plus the Kubernetes libraries for lists, regular expressions, quantities and formats.
- `absent: true` passes once the object is gone.

For a list, `list.selector` selects objects of one kind in the scenario namespace:

- `count` is the exact number of objects; `count: 0` asserts that nothing is left.
- `cel` expressions see `items`, the list of objects.

The step polls every two seconds until every check holds or `timeout` passes, and then reports the last reason it did not.

### exec

`exec` runs `command` in `container` of one pod: `pod` names it, or `selector` picks the first Running pod by name.
`expect.contains` lists strings that must all appear in the command's standard output.
A non-zero exit or missing text is retried until `timeout`.

Write shell pipelines so they print a labelled value, for example `echo ok=$n`, and expect that label.
A bare number in `contains` would also match a substring of a bigger number.

### delete

`delete` removes one `object` or every object a `list` selector matches, in the foreground, and waits until each is gone or has been recreated under a new UID.
That second case is how a StatefulSet replaces a deleted pod under the same name, so the step returns as soon as the replacement exists.
Deleting an object that is already gone is fine.
A list delete that matches nothing is an error, because a selector with a typo would otherwise pass.

### go

`go.name` is a key of the registry in `test/e2e/steps_test.go`, and `go.args` are strings the function receives.
Whatever the function returns is merged into the scenario's variables.
Use a Go step when a check cannot be written as an assert or an exec, for example reading a role from Valkey itself and handing the pod name to later steps.
Every registered Go step MUST be used by at least one scenario; `task test` fails otherwise.

## Templating

These fields are rendered with Go's text/template before they are used: `object.name`, `list.selector`, `exec.pod`, `exec.selector`, each entry of `exec.command` and `expect.contains`, the values of `go.args`, and the text of an `apply` manifest.

- `{{ .Namespace }}` is the scenario's namespace, for example in a Service FQDN.
- `{{ .Version }}` is the operator version the run resolved.
- `{{ .Vars.<key> }}` is a value a `go` step returned.

A missing variable is an error, not an empty string.
CEL expressions, labels, step names, timeouts, and `match` are never templated.

## Timeouts, labels, and priority

Every step has a `timeout`, two minutes when omitted.
It bounds the whole step, including every retry.
Give a readiness wait what the operator needs; the Valkey pack gives a cluster ten minutes to form on a cold node.

`labels` are Ginkgo labels and must not contain `&`, `|`, `!`, `,`, `(`, `)`, `/`, or start with `:`.
`chaos` is reserved for scenarios that disturb the system under test; the pull request gate skips them.
Give a scenario that runs for two minutes or more a `priority`: `3` for about four minutes and up, `2` for about three, `1` for about two.

## Reusing a block

YAML anchors work inside one file.
The Valkey scenarios define their readiness block once as `cel: &ready` and reuse it as `cel: *ready` after every change.

## What the parser rejects

A scenario is parsed and validated before any cluster is touched, in `task test` as well as at the start of a run.
It rejects an unknown field, a step with zero or two kinds, a label Ginkgo cannot filter on, an unregistered Go step, a `match` value that would match anything, a CEL expression that is not a boolean, a template that does not parse, a manifest that does not exist, and a name whose namespace would exceed 63 characters.
Every broken step is reported, with its file and position.
