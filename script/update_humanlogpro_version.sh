#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

function main() {
    export SHA=${1}
    export GOPRIVATE="github.com/humanlogio/"
    go get -u github.com/humanlogio/humanlog-pro@${SHA}
    go mod tidy
}

main ${1}
