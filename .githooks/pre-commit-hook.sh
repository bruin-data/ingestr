#!/usr/bin/env bash

set -euo pipefail

echo "scanning for secrets ..."

WORK_DIR="/root/code"

secret_detected() {
    echo "secrets detected in source code. commit aborted."
    exit 1
}

# use gitleaks binary if available
# else fallback to using docker for running gitleaks
CMD="gitleaks protect --staged -v"

if [[ ! `which gitleaks`  ]]; then
    which docker > /dev/null || (echo "gitleaks or docker is required for running secrets scan." && exit 1)
    CMD="docker run -v $PWD:$WORK_DIR -w $WORK_DIR --rm ghcr.io/gitleaks/gitleaks:latest protect --staged -v"
fi

$CMD || secret_detected
