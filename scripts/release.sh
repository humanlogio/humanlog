#!/usr/bin/env bash

usage() {
    echo "USAGE: ./release.sh [version] [msg...]"
    exit 1
}

REVISION=$(git rev-parse HEAD)
GIT_TAG=$(git name-rev --tags --name-only $REVISION)
if [ "$GIT_TAG" = "" ]; then
    GIT_TAG="devel"
fi


VERSION=$1
if [ "$VERSION" = "" ]; then
    echo "Need to specify a version! Perhaps '$GIT_TAG'?"
    usage
fi

set -u -e

rm -rf /tmp/humanlog_build/

mkdir -p /tmp/humanlog_build/linux
GOOS=linux go build -ldflags "-X main.version=$VERSION" -o /tmp/humanlog_build/linux/humanlog ../cmd/humanlog
pushd /tmp/humanlog_build/linux/
tar cvzf /tmp/humanlog_build/humanlog_linux.tar.gz humanlog
popd

mkdir -p /tmp/humanlog_build/darwin
GOOS=darwin go build -ldflags "-X main.version=$VERSION" -o /tmp/humanlog_build/darwin/humanlog ../cmd/humanlog
pushd /tmp/humanlog_build/darwin/
tar cvzf /tmp/humanlog_build/humanlog_darwin.tar.gz humanlog
popd

temple file < README.tmpl.md > ../README.md -var "version=$VERSION"


git add ../README.md
git commit -m 'release bump'

hub release create \
    -a /tmp/humanlog_build/humanlog_linux.tar.gz \
    -a /tmp/humanlog_build/humanlog_darwin.tar.gz \
    $VERSION

git push origin master
