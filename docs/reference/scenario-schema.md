# Scenario schema

A scenario file has the API version `testbench.dev/v1alpha1` and the kind `Scenario`.
The file name is the scenario name.
The name is a DNS label of 30 characters or less.
The loader reads the file with strict decoding.
An unknown field is an error.

## Top level

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `apiVersion` | string | yes | Must be `testbench.dev/v1alpha1`. |
| `kind` | string | yes | Must be `Scenario`. |
| `labels` | list of strings | no | Ginkgo labels. A label must not contain `&`, `\|`, `!`, `,`, `(`, `)` or `/`. A label must not start with `:`. The same label must not appear twice. The engine adds the pack name as a label. |
| `priority` | integer | no | The Ginkgo spec priority. A higher value starts earlier in a parallel run. The default is 0. The value must not be negative. |
| `steps` | list of steps | yes | At least one step. |

## Step

A step has one `name`, an optional `timeout`, and exactly one kind field.

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `name` | string | yes | Shown in the report with the step position. |
| `timeout` | duration | no | A Go duration such as `10m`. Bounds the whole step, with all retries. The default is `2m`. The value must be positive. |
| `apply` | object | one of | See `apply`. |
| `assert` | object | one of | See `assert`. |
| `exec` | object | one of | See `exec`. |
| `delete` | object | one of | See `delete`. |
| `go` | object | one of | See `go`. |

## apply

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `file` | string | yes | A path relative to the pack directory. The file must exist. The path must not leave the pack. |

The engine renders the file as a template, then applies each document with server-side apply.
A namespaced object without a namespace goes into the scenario namespace.
A namespaced object with another namespace is an error.
Each document must have `apiVersion`, `kind` and `metadata.name`.

## assert

Set exactly one of `object` and `list`.

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `object` | object reference | one of | The object to read. Fields: `apiVersion`, `kind`, `name`. The name is a template. |
| `list` | list reference | one of | The objects to list in the scenario namespace. Fields: `apiVersion`, `kind`, `selector`. The selector is a label selector or a template. |
| `match` | object | no | Object only. A subset the live object must contain. An empty string, an empty list, an empty map or `null` is an error. |
| `cel` | list of strings | no | Boolean CEL expressions. `self` is the object. `items` is the list. Every expression must be true at the same time. |
| `count` | integer | no | List only. The exact number of objects. The value must not be negative. `count: 0` asserts that no object is left. |
| `absent` | boolean | no | Object only. The step passes when the object is not found. Do not combine with `match` or `cel`. |

A list assert needs `count` or `cel`.
The step retries every 2 seconds until the timeout.

## exec

Set exactly one of `pod` and `selector`.

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `pod` | string | one of | The pod name. A template. |
| `selector` | string | one of | A label selector. The engine uses the first Running pod, sorted by name. |
| `container` | string | yes | The container name. |
| `command` | list of strings | yes | The command and its arguments. Each entry is a template. |
| `expect.contains` | list of strings | no | Each string must appear in the standard output. Each entry is a template. An empty string is an error. |

The engine runs the command with `kubectl exec`.
One attempt has 30 seconds.
The engine retries a non-zero exit or missing text until the timeout.

## delete

Set exactly one of `object` and `list`.

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `object` | object reference | one of | The object to delete. The name is a template. |
| `list` | list reference | one of | The objects to delete in the scenario namespace. The selector must match at least one object. |

The engine deletes in the foreground.
The step waits until each object is gone, or exists with a new UID.
An object that is already gone is not an error.

## go

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `name` | string | yes | A key of the registry in `test/e2e/steps_test.go`. |
| `args` | map of strings | no | The arguments. Each value is a template. A key must not be empty. |

The returned values go into the variables of the scenario.

## Templates

These fields are templates: `object.name`, `list.selector`, `exec.pod`, `exec.selector`, each `exec.command` entry, each `expect.contains` entry, each `go.args` value, and the content of an `apply` file.
A template can use `{{ .Namespace }}`, `{{ .Version }}` and `{{ .Vars.<key> }}`.
A missing variable is an error.
These fields are not templates: `cel`, `labels`, `name`, `timeout` and `match`.
