#!/bin/sh
# Pulls every workload image the pack's manifests name once and loads it into
# the kind cluster, so three nodes do not each pull it from Docker Hub. Hosted
# CI runners share egress addresses and the anonymous pull limit is per address.
set -eu

pack=${1:?usage: preload-images.sh <pack> [cluster]}
cluster=${2:-testbench}

cd "$(dirname "$0")/../.."

dir="operators/$pack/manifests"
[ -d "$dir" ] || { echo "preload-images: no manifests under $dir" >&2; exit 1; }

# With the containerd image store, a pull fetches only the runner's platform,
# while `kind load docker-image` exports the whole multi-arch index and its
# import then misses the other platforms' blobs. Saving one platform to an
# archive keeps the import to what was pulled.
platform="linux/$(docker version --format '{{.Server.Arch}}')"
archive=$(mktemp)
trap 'rm -f "$archive"' EXIT

# Any `image:` key, at any depth, in any manifest of the pack.
grep -rhoE '^[[:space:]]*image:[[:space:]]*[^[:space:]]+' "$dir" \
  | awk '{print $2}' | tr -d '"'"'" | sort -u \
  | while IFS= read -r img; do
      echo "preload-images: $img ($platform)"
      docker pull --platform "$platform" "$img"
      docker save --platform "$platform" -o "$archive" "$img"
      kind load image-archive "$archive" --name "$cluster"
    done
