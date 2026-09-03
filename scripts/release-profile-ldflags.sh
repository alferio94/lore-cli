#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
  echo "usage: $0 PROFILE.json" >&2
  exit 2
fi

exec go run ./internal/releaseprofile/cmd/profileldflags "$1"
