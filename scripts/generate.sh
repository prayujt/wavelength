#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
GO=${GO:-go}
PROTOC=${PROTOC:-protoc}
if ! command -v "$PROTOC" >/dev/null 2>&1; then
  if [ -x .tools/protoc/bin/protoc ]; then
    PROTOC=.tools/protoc/bin/protoc
  else
    echo 'Install protoc (tested: 30.2), or set PROTOC to its executable.' >&2
    exit 1
  fi
fi
mkdir -p .tools/bin gen
GOBIN="$PWD/.tools/bin" "$GO" install google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.6
GOBIN="$PWD/.tools/bin" "$GO" install connectrpc.com/connect/cmd/protoc-gen-connect-go@v1.18.1
"$PROTOC" -I proto \
  --plugin=protoc-gen-go=.tools/bin/protoc-gen-go \
  --plugin=protoc-gen-connect-go=.tools/bin/protoc-gen-connect-go \
  --go_out=gen --go_opt=paths=source_relative \
  --go_opt=Mgoogle/protobuf/timestamp.proto=google.golang.org/protobuf/types/known/timestamppb \
  --connect-go_out=gen --connect-go_opt=paths=source_relative \
  proto/wavelength/v1/library.proto proto/wavelength/v1/ingestion.proto
