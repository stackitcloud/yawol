#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

chart_name=yawol-controller
helm_artifacts=artifacts/charts
rm -rf "$helm_artifacts"
mkdir -p "$helm_artifacts"

function image_registry() {
  echo "$1" | cut -d '/' -f -2
}

function image_repo() {
  echo "$1" | cut -d ':' -f 1
}

function image_tag() {
  git describe --tag --always --dirty
}

## HELM
cp -r charts/${chart_name} "$helm_artifacts"
yq -i "\
  ( .image.repository = \"$(image_repo "$1")\" ) | \
  ( .image.tag = \"$(image_tag "$1")\" )\
" "$helm_artifacts/${chart_name}/values.yaml"

# push to registry
if [ "$PUSH" != "true" ] ; then
  echo "Skip pushing artifacts because PUSH is not set to 'true'"
  exit 0
fi

helm package "$helm_artifacts/${chart_name}" --version "$(image_tag "$1")" -d "$helm_artifacts" > /dev/null 2>&1
helm push "$helm_artifacts/${chart_name}-"* "oci://$(image_registry "$1")/charts"
