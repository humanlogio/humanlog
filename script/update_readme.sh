#!/usr/bin/env bash

basedir=$(git rev-parse --show-toplevel)

usage() {
    echo "USAGE: update_readme.sh [version]"
    exit 1
}

version=$1
if [ "$version" = "" ]; then
    echo "Need to specify a version!"
    usage
fi


set -e -u -x

temple file < ${basedir}/script/README.tmpl.md > ${basedir}/README.md -var "version=$version"