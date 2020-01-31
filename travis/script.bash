#!/usr/bin/env bash
set -eux

if [ "${PGVERSION-}" != "" ]
then
  go test -v -race ./...
fi