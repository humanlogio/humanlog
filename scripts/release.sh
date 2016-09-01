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

rm -rf /tmp/temple_build/

mkdir -p /tmp/temple_build/linux
GOOS=linux godep go build -ldflags "-X main.version=$VERSION" -o /tmp/temple_build/linux/temple ../
pushd /tmp/temple_build/linux/
tar cvzf /tmp/temple_build/temple_linux.tar.gz temple
popd

mkdir -p /tmp/temple_build/darwin
GOOS=darwin godep go build -ldflags "-X main.version=$VERSION" -o /tmp/temple_build/darwin/temple ../
pushd /tmp/temple_build/darwin/
tar cvzf /tmp/temple_build/temple_darwin.tar.gz temple
popd

temple file < README.tmpl.md > ../README.md -var "version=$VERSION"
git add ../README.md
git commit -m 'release bump'

hub release create \
    -a /tmp/temple_build/temple_linux.tar.gz \
    -a /tmp/temple_build/temple_darwin.tar.gz \
    $VERSION

git push origin master
