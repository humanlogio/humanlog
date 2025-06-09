#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

state_hack="ensure-exists"

${root}/script/dev-runclean.sh "go run ./cmd/humanlog state hack ${state_hack} && curl -sSL -k https://host.docker.internal:3000/install.sh | HUMANLOG_DEBUG=true bash && echo 'done' && bash"
