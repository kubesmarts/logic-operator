#!/usr/bin/env bash
set -euo pipefail

cluster_name=${1:?Usage: load-kind-image.sh CLUSTER IMAGE [KIND_BIN]}
image=${2:?Usage: load-kind-image.sh CLUSTER IMAGE [KIND_BIN]}
kind_bin=${3:-kind}

node=$("$kind_bin" get nodes --name "$cluster_name" | head -n 1)
if [[ -z "$node" ]]; then
	echo "No nodes found for Kind cluster '$cluster_name'" >&2
	exit 1
fi

case "$(docker exec "$node" uname -m)" in
	x86_64|amd64) platform=linux/amd64 ;;
	aarch64|arm64) platform=linux/arm64 ;;
	*)
		echo "Unsupported Kind node architecture: $(docker exec "$node" uname -m)" >&2
		exit 1
		;;
esac

echo "Saving $image for $platform to load into $cluster_name"
if docker image save --help 2>&1 | grep -q -- '--platform'; then
	archive=$(mktemp "${TMPDIR:-/tmp}/kind-image.XXXXXX.tar")
	trap 'rm -f "$archive"' EXIT
	docker image save --platform "$platform" --output "$archive" "$image"
	"$kind_bin" load image-archive "$archive" --name "$cluster_name"
else
	echo "Docker image save lacks --platform; importing into $node containerd without --all-platforms"
	docker image save "$image" |
		docker exec -i "$node" ctr --namespace=k8s.io images import --platform "$platform" --digests -
fi
