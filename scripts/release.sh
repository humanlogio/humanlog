#!/usr/bin/env bash

basedir=$(git rev-parse --show-toplevel)

usage() {
    echo "USAGE: release.sh [version] [msg...]"
    exit 1
}

command -v goreleaser >/dev/null 2>&1 || { echo "Required: goreleaser. Install it https://github.com/goreleaser/goreleaser.  Aborting." >&2; exit 1; }

if [ "$GITHUB_TOKEN" = "" ]; then
    echo "Need to specify a GITHUB_TOKEN!"
    usage
fi

version=$1
if [ "$version" = "" ]; then
    echo "Need to specify a version!"
    usage
fi

shift
msg=$@
if [ "$msg" = "" ]; then
    echo "Need to specify a message!"
    usage
fi

set -e -u -x

temple file < $basedir/scripts/README.tmpl.md > $basedir/README.md -var "version=$version"

git add $basedir/README.md
git commit -m "$msg"
git tag -a $version -m "$msg"

goreleaser --rm-dist --config $basedir/goreleaser.yaml

git push origin master
