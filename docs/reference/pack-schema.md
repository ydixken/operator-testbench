# Pack schema

The file `operators/<pack>/operator.yaml` has the API version `testbench.dev/v1alpha1` and the kind `OperatorPack`.
The directory name is the pack name.
The name is a DNS label of 16 characters or less.
The loader reads the file with strict decoding.
An unknown field is an error.

## Top level

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `apiVersion` | string | yes | Must be `testbench.dev/v1alpha1`. |
| `kind` | string | yes | Must be `OperatorPack`. |
| `operator` | object | yes | See `operator`. |
| `install` | object | yes | See `install`. |

## operator

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `version` | string | yes | The default operator version. One token, without spaces or `/`. `TESTBENCH_OPERATOR_VERSION` replaces it. Templates read it as `{{ .Version }}`. |
| `namespace` | string | yes | A DNS label. The engine creates this namespace and installs the operator in it. |
| `selector` | string | yes | A label selector for the controller pods in `namespace`. The engine waits for a Ready pod with it, and reads logs with it. |

## install

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `crds` | list of strings | no | HTTPS URLs of CRD manifests. Each URL is a template with `{{ .Version }}`. The engine applies them server-side before the chart. |
| `helm` | object | yes | See `helm`. |

## helm

| Field | Type | Required | Rule |
|-------|------|----------|------|
| `repo` | string | yes | An HTTPS URL of a Helm repository. |
| `chart` | string | yes | The chart name. |
| `version` | string | yes | The chart version. |
| `release` | string | yes | A DNS label. The release name. It is also the lock of the run. |
| `set` | map of strings | no | Values for `--set`. Each value is a template with `{{ .Version }}`. A key must not be empty. |

The engine runs `helm install <release> <chart> --repo <repo> --version <version> -n <namespace> --skip-crds --wait --timeout 5m`, with one `--set` per entry.

## Renovate comments

Put a `# renovate:` comment on the line before `operator.version` and before `helm.version`.
One Renovate manager then updates every pack.
Example: `# renovate: datasource=github-releases depName=valkey-io/valkey-operator`.
