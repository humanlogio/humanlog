#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

${root}/script/dev-runclean.sh "export CI=true && curl -sSL -k https://host.docker.internal:3000/install_apictl.sh | bash && echo 'done' && bash"
