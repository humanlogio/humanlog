#!/usr/bin/env bash

set -euox pipefail

root=$(git rev-parse --show-toplevel)

gotags="${HUMANLOG_GOTAGS:-}"
goos="${GOOS:-$(go env GOOS)}"
goarch="${GOARCH:-$(go env GOARCH)}"
is_prod_build="${HUMANLOG_IS_PROD_BUILD:-}"
output_dir="${HUMANLOG_OUTDIR:-${root}/dist}"
output_bin="${HUMANLOG_OUTBIN:-humanlog}"
output_path="${output_dir}/${output_bin}"

function latest_git_tag() {
    git describe --tags $(git rev-list --tags --max-count=1)
}

function git_commit_timestamp() {
    git show --no-patch --format=%ct
}

function git_short_commit() {
    git rev-parse --short HEAD
}

function current_version() {
    apictl version to-json --version $(latest_git_tag) |
        apictl version math build set $(git_short_commit)
}

function next_patch_version() {
    current_version |
        apictl version math patch add 1 |
        apictl version math pre add next |
        apictl version math pre add $(git_commit_timestamp)
}

function get_major() {
    jq -r '.major // 0'
}

function get_minor() {
    jq -r '.minor // 0'
}

function get_patch() {
    jq -r '.patch // 0'
}

function get_pre() {
    jq -r '.prereleases // [] | join(".")'
}

function get_build() {
    jq -r '.build // ""'
}

function main() {
    local major=""
    local minor=""
    local patch=""
    local pre=""
    local build=""
    local version=""
    if [[ "${is_prod_build}" != "true" ]]; then
        echo "setting version to $(next_patch_version | apictl version from-json)"
        major=$(next_patch_version | get_major)
        minor=$(next_patch_version | get_minor)
        patch=$(next_patch_version | get_patch)
        pre=$(next_patch_version | get_pre)
        build=$(next_patch_version | get_build)
        version=$(next_patch_version | apictl version from-json)
    else
        major=$(current_version | get_major)
        minor=$(current_version | get_minor)
        patch=$(current_version | get_patch)
        build=$(current_version | get_build)
        version=$(current_version | apictl version from-json)
    fi
    local tarname="humanlog_${version}_${goos}_${goarch}.tar.gz"

    local flags="-o ${output_path} -trimpath"
    if [[ ! -z "${gotags}" ]]; then
        flags+=" -tags ${gotags}"
    fi

    ldflags="-s -w"

    if [[ "${goos}" == "linux" ]]; then
        ldflags+=$" -extldflags '-lc -lrt -lpthread --static'"
    fi

    ldflags+=" -X main.versionMajor=${major}"
    ldflags+=" -X main.versionMinor=${minor}"
    ldflags+=" -X main.versionPatch=${patch}"
    ldflags+=" -X main.versionBuild=${build}"
    if [[ "${is_prod_build}" != "true" ]]; then
        ldflags+=" -X main.versionPrerelease=${pre}"
        ldflags+=" -X main.defaultApiAddr=https://api.humanlog.dev"
        ldflags+=" -X main.defaultBaseSiteAddr=https://humanlog.dev"
        ldflags+=" -X main.defaultReleaseChannel=dev"
    else
        ldflags+=" -X main.versionPrerelease="
        ldflags+=" -X main.defaultApiAddr=https://api.humanlog.io"
        ldflags+=" -X main.defaultBaseSiteAddr=https://humanlog.io"
        ldflags+=" -X main.defaultReleaseChannel=main"
    fi

    flags+=" -ldflags=\"${ldflags}\""

    eval go build ${flags} ./cmd/humanlog

    cd ${output_dir}
    tar -zcvf ${tarname} humanlog
}

main
