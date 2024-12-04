#!/usr/bin/env bash

set -euo pipefail

WORK_DIR="/root/code"


if [[ ! `which docker` ]]; then
    echo "docker is required to run gitleaks"
    echo "for vulnerability scanning. commit aborted."
    exit 1
fi

docker run \
    -v "$PWD:$WORK_DIR" \
    -w "$WORK_DIR" \
    ghcr.io/gitleaks/gitleaks:latest dir -v

if [[ $? -ne 0 ]]; then
    echo "secrets detected in source code. commit aborted."
    exit 1
fi