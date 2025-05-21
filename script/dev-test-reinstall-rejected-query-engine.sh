#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

state_hack="record-last-prompted-to-enable-localhost-now"

${root}/script/dev-runclean.sh "go run ./cmd/humanlog state hack ${state_hack} && curl -sSL -k https://host.docker.internal:3000/install.sh | bash && echo 'done' && bash"
