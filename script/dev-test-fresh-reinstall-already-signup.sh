#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

echo "todo"
exit 1

${root}/script/dev-runclean.sh "curl -sSL -k https://host.docker.internal:3000/install.sh | bash && echo 'done' && bash"
