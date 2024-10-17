#!/usr/bin/env bash

set -euox pipefail

root=$(git rev-parse --show-toplevel)

function list_archives() {
    jq < dist/artifacts.json -r '.[] | select(.type == "Archive") | .name, .path, .goos, .goarch'
}

function handle_archive() {
    while read -r filename; read -r path; read -r goos; read -r goarch; do
        local url=$(get_archive_url ${filename})
        if [ -z "${url}" ]; then
            echo "no archive for ${filename}";
            exit 1;
        fi

        local sig=$(get_signature ${root}/${path})

        local extra_flags=""

        if [ -f dist-extra/version.json ]; then
            extra_flags="--pre $(get_prerelease) --build $(get_build)"
        fi

        apictl --api.url ${api_url} create version-artifact \
            --project ${project} \
            --major $(get_version_major) \
            --minor $(get_version_minor) \
            --patch $(get_version_patch) \
            --sha256 $(get_sha256sum ${path}) \
            --url ${url} \
            --os ${goos} \
            --arch ${goarch} \
            --sig ${sig} ${extra_flags}
    done
}

function get_archive_url() {
    local filename=${1}
    if [ -f dist-extra/version.json ]; then
        echo $(jq < dist-extra/version.json -r '.archive_base_url')/${filename}
    else
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
    fi
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
    if [ -f dist-extra/version.json ]; then
        jq < dist-extra/version.json -r '.major'
    else
        major=`echo ${tag} | cut -d. -f1`
        echo "${major}"
    fi
}

function get_version_minor() {
    if [ -f dist-extra/version.json ]; then
        jq < dist-extra/version.json -r '.minor'
    else
        minor=`echo ${tag} | cut -d. -f2`
        echo "${minor}"
    fi
}

function get_version_patch() {
    if [ -f dist-extra/version.json ]; then
        jq < dist-extra/version.json -r '.patch'
    else
        patch=`echo ${tag} | cut -d. -f3`
        echo "${patch}"
    fi

}

function get_version() {
    if [ -f dist-extra/version.json ]; then
        jq < dist-extra/version.json -r '.version'
    else
        jq < dist/metadata.json -r '.version'
    fi
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
