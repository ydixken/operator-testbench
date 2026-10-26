# syntax=docker/dockerfile:1
# The testbench as one image: the compiled e2e suite, the ginkgo CLI that runs
# it in parallel, and kubectl for the exec steps and the controller log tail.
# No helm, because cluster mode installs nothing; no shell, because the chart
# calls ginkgo with its arguments. One image fits only the operator versions
# its packs pin, so a release has no latest tag.

# Cross-compiles on the build platform, so a multi-arch build needs no emulation.
FROM --platform=$BUILDPLATFORM golang:1.27.3-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
# renovate: datasource=github-releases depName=kubernetes/kubernetes
ARG KUBECTL_VERSION=v1.37.1
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# go test -c is what ginkgo build runs; the CLI comes at the version go.mod pins.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go test -c -o /out/e2e.test ./test/e2e \
 && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o /out/ginkgo github.com/onsi/ginkgo/v2/ginkgo
RUN curl -fsSLo /out/kubectl "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/$TARGETOS/$TARGETARCH/kubectl" \
 && echo "$(curl -fsSL "https://dl.k8s.io/release/$KUBECTL_VERSION/bin/$TARGETOS/$TARGETARCH/kubectl.sha256")  /out/kubectl" \
    | sha256sum -c - \
 && chmod 0755 /out/kubectl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ginkgo /out/kubectl /usr/local/bin/
COPY --from=build /out/e2e.test /testbench/e2e.test
WORKDIR /testbench
ENTRYPOINT ["ginkgo"]
# The smallest useful run; the chart passes its own arguments.
CMD ["-v", "--fail-on-empty", "--label-filter=!chaos", "e2e.test", "--", "-test.run=^TestE2E$"]
