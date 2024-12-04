#!/usr/bin/env bash
WORK_DIR="/root/code"

docker run \
    -v "$PWD:$WORK_DIR" \
    -w "$WORK_DIR" \
    ghcr.io/gitleaks/gitleaks:latest dir -v

if [[ $? ]]; then
    echo "secrets detected in source code. commit aborted."
    exit 1
fi