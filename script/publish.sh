#!/usr/bin/env bash

set -euox pipefail

root=$(git rev-parse --show-toplevel)

is_prod_build="${IS_PROD_BUILD:-}"
api_url="${API_URL:-"https://api.humanlog.io"}"
channel="${CHANNEL:-main}"
project="${PROJECT:-humanlog}"
bucket="${BUCKET:-humanlog-binaries}"
region="${BUCKET_REGION:-sfo3}"
endpoint="${BUCKET_ENDPOINT:-https://humanlog-binaries.sfo3.digitaloceanspaces.com}"
cdn_endpoint="${BUCKET_CDN_ENDPOINT:-https://humanlog-binaries.sfo3.cdn.digitaloceanspaces.com}"

function main() {
    local version=$(get_version | apictl version from-json)
    local major=$(get_version | get_major)
    local minor=$(get_version | get_minor)
    local patch=$(get_version | get_patch)
    local build=$(get_version | get_build)
    local pre=""
    if [[ "${is_prod_build}" != "true" ]]; then
        pre=$(get_version | get_pre)
    fi

    local bucketdir="${project}/${channel}/$(git_commit_timestamp)/$(git_short_commit)"

    for dir in $(find . -type d -name "humanlog-bins-*-*"); do
        local os=$(echo "${dir}" | sed 's/\.\/humanlog-bins-\(.*\)-\(.*\)/\1/')
        local arch=$(echo "${dir}" | sed 's/\.\/humanlog-bins-\(.*\)-\(.*\)/\2/')

        local filename="humanlog_${version}_${os}_${arch}.tar.gz"
        local bucketpath="${bucketdir}/${filename}"
        local download_url="${cdn_endpoint}/${bucket}/${bucketpath}"
        local sha256sum=$(get_sha256sum ${dir}/${filename})

        local extra_flags="--build ${build}"
        if [[ "${pre}" != "" ]]; then
            extra_flags+=" --pre ${pre}"
        fi

        apictl --api.url ${api_url} create s3-artifact \
            --filepath ${dir}/${filename} \
            --s3.access_key $AWS_ACCESS_KEY_ID \
            --s3.secret_key $AWS_SECRET_ACCESS_KEY \
            --s3.endpoint ${endpoint} \
            --s3.region ${region} \
            --s3.bucket ${bucket} \
            --s3.use_path_style=true \
            --s3.directory ${bucketpath}

        apictl --api.url ${api_url} create version-artifact \
            --project ${project} \
            --major ${major} \
            --minor ${minor} \
            --patch ${patch} \
            --sha256 ${sha256sum} \
            --url ${download_url} \
            --os ${os} \
            --arch ${arch} \
            --sig "no-signature" ${extra_flags}
    done

    apictl --api.url ${api_url} create published-version \
        --project ${project} \
        --channel ${channel} \
        --version ${version}
}

function latest_git_tag() {
    git describe --tags $(git rev-list --tags --max-count=1)
}

function git_commit_timestamp() {
    git show --no-patch --format=%ct
}

function git_short_commit() {
    git rev-parse --short HEAD
}

function get_version() {
    if [[ "${is_prod_build}" != "true" ]]; then
        next_patch_version
    else
        current_version
    fi
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

function get_sha256sum() {
    local filename=${1}
    shasum -a 256 ${filename} | cut -d " " -f 1
}

main
