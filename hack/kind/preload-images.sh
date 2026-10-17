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

# Any `image:` key, at any depth, in any manifest of the pack.
grep -rhoE '^[[:space:]]*image:[[:space:]]*[^[:space:]]+' "$dir" \
  | awk '{print $2}' | tr -d '"'"'" | sort -u \
  | while IFS= read -r img; do
      echo "preload-images: $img"
      docker pull "$img"
      kind load docker-image "$img" --name "$cluster"
    done
