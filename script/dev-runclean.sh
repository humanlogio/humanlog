#!/usr/bin/env bash

set -euo pipefail

root=$(git rev-parse --show-toplevel)

function check_ssh_agent_running() {
    if [[ -z "${SSH_AUTH_SOCK-}" ]]; then
        echo "Error: SSH_AUTH_SOCK is not set. Start your ssh-agent first." >&2
        exit 1
    fi

    if [[ ! -S "${SSH_AUTH_SOCK}" ]]; then
        echo "Error: SSH_AUTH_SOCK (${SSH_AUTH_SOCK}) is not a socket. Is ssh-agent running?" >&2
        exit 1
    fi
}

function check_vendor() {
    # # with vendor
    # ```
    # ________________________________________________________
    # Executed in   22.35 secs      fish           external
    #    usr time   17.84 millis  124.00 micros   17.71 millis
    #    sys time   20.59 millis  797.00 micros   19.79 millis
    # ```
    # # without vendor
    # ```
    # ________________________________________________________
    # Executed in   63.36 secs      fish           external
    #    usr time   28.74 millis    0.24 millis   28.50 millis
    #    sys time   34.66 millis    1.58 millis   33.08 millis
    # ```
    if [[ ! -d "${root}/vendor" ]]; then
        echo "Warning: no vendor directory found; Go commands will be much slower without vendored dependencies." >&2
    fi
}

function run_in_docker() {
    local cmd="$1"
    local ssh_agent_socket="${SSH_AUTH_SOCK}"
    docker run --rm -it \
        -v "${root}:/go/src/app" \
        -v "${ssh_agent_socket}:/ssh-agent" \
        -e SSH_AUTH_SOCK=/ssh-agent \
        -e GIT_SSH_COMMAND="ssh -A -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null" \
        -e GOPRIVATE="github.com/humanlog.io/" \
        -w /go/src/app \
        golang:1.24 \
        bash -lc "git config --global url.\"ssh://git@github.com/\".insteadOf https://github.com/ && \
        export PATH=\$PATH:/usr/local/go/bin/ && \
        ${cmd}"
}

function main() {
    if [[ $# -lt 1 ]]; then
        echo "Usage: $0 '<command-to-run>'" >&2
        exit 1
    fi
    check_ssh_agent_running
    check_vendor
    run_in_docker "$1"
}

main "$@"

# example:
# ./script/dev-runclean.sh "go run -tags pro ./cmd/humanlog version check"
