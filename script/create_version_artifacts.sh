#!/usr/bin/env bash

set -euox pipefail

root=$(git rev-parse --show-toplevel)

function list_archives() {
    jq < dist/artifacts.json -r '.[] | select(.type == "Archive") | .name, .path, .goos, .goarch'
}

function handle_archive() {
    while read -r filename; read -r path; read -r goos; read -r goarch; do
        local url=$(get_archive_url ${filename})
        if [ -z "${url}" ]; then echo "no archive for ${filename}"; continue; fi

        local sig=$(get_signature ${root}/${path})

        apictl --api.url ${api_url} create version-artifact \
            --project ${project} \
            --major $(get_version_major) \
            --minor $(get_version_minor) \
            --patch $(get_version_patch) \
            --sha256 $(get_sha256sum ${path}) \
            --pre $(get_prerelease) \
            --build $(get_build) \
            --url ${url} \
            --os ${goos} \
            --arch ${goarch} \
            --sig ${sig}
    done
}

function get_archive_url() {
    local filename=${1}
    gh api graphql -F owner=${owner} -F name=${project} -F tag="v${tag}" -F filename=${filename} -f query='
            query($name: String!, $owner: String!, $tag: String!, $filename: String!) {
                repository(name: $name, owner: $owner) {
                    release(tagName: $tag) {
                        releaseAssets(first: 1, name: $filename) {
                            nodes {
                                downloadUrl
                            }
                        }
                    }
                }
            }' --jq '.data.repository.release.releaseAssets.nodes[0].downloadUrl'
}

function get_sha256sum() {
    local filename=${1}
    shasum -a 256 ${filename} | cut -d " " -f 1
}

function get_signature() {
    local filename=${1}.sig
    if [ -f "${filename}" ]; then cat < ${filename};
    else echo "no-signature"; fi
}

function get_version_major() {
    major=`echo ${tag} | cut -d. -f1`
    echo "${major}"
}

function get_version_minor() {
    minor=`echo ${tag} | cut -d. -f2`
    echo "${minor}"
}

function get_version_patch() {
    patch=`echo ${tag} | cut -d. -f3`
    echo "${patch}"
}

function get_version() {
    jq < dist-extra/version.json -r '.version'
}

function get_project_name() {
    jq < dist/metadata.json -r '.project_name'
}

function get_prerelease() {
    jq < dist-extra/version.json -r '.pre'
}

function get_build() {
    jq < dist-extra/version.json -r '.build'
}

function get_channel() {
    local channel=${CHANNEL:-main}
    echo ${channel}
}

function main() {
    owner=humanlogio
    api_url=${API_URL:-"https://api.humanlog.io"}
    project=$(get_project_name)
    tag=$(get_version)
    channel=$(get_channel)

    list_archives | handle_archive

    apictl --api.url ${api_url} create published-version \
            --project ${project} \
            --channel ${channel} \
            --version $(get_version)
}

main
