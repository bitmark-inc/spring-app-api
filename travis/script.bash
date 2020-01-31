#!/usr/bin/env bash
set -eux

go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...