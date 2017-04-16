#!/usr/bin/env bash

basedir=$(git rev-parse --show-toplevel)

usage() {
    echo "USAGE: release.sh [version] [msg...]"
    exit 1
}

VERSION=$1
if [ "$VERSION" = "" ]; then
    echo "Need to specify a version!"
    usage
fi

shift
MSG=$@
if [ "$MSG" = "" ]; then
    echo "Need to specify a message!"
    usage
fi

set -e -u -x

temple file < $basedir/scripts/README.tmpl.md > $basedir/README.md -var "version=$VERSION"

git add $basedir/README.md
git commit -m "$MSG"
git tag -a $VERSION -m "$MSG"

goreleaser --config $basedir/goreleaser.yaml

git push origin master
