#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

tool_dir="$project_root/.tools/bin"

check_version() {
    local actual="$1"
    local expected="$2"

    if [[ "$actual" != "$expected" ]]; then
        printf '工具版本不匹配，期望 %s，实际 %s\n' \
            "$expected" "$actual" >&2
        exit 1
    fi
}

if [[ ! -f proto/probe/v1/probe.proto || ! -f proto/auth/v1/auth.proto ]]; then
    printf '缺少协议文件，请先执行 git submodule update --init --recursive\n' >&2
    exit 1
fi

check_version "$(protoc --version)" "libprotoc 36.1"
check_version "$("$tool_dir/protoc-gen-go" --version)" "protoc-gen-go v1.36.11"
check_version "$("$tool_dir/protoc-gen-go-grpc" --version)" "protoc-gen-go-grpc 1.6.2"

mkdir -p gen

protoc \
    --proto_path=proto \
    --plugin="protoc-gen-go=$tool_dir/protoc-gen-go" \
    --plugin="protoc-gen-go-grpc=$tool_dir/protoc-gen-go-grpc" \
    --go_out=gen \
    --go_opt=paths=source_relative \
    --go-grpc_out=gen \
    --go-grpc_opt=paths=source_relative \
    proto/probe/v1/probe.proto \
    proto/auth/v1/auth.proto
