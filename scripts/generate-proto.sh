#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"

tool_dir="$project_root/.tools/bin"
# 本地联调可显式指定协议工作树，默认仍使用固定子模块
proto_dir="${REALMMESH_PROTO_DIR:-$project_root/proto}"

check_version() {
    local actual="$1"
    local expected="$2"

    if [[ "$actual" != "$expected" ]]; then
        printf '工具版本不匹配，期望 %s，实际 %s\n' \
            "$expected" "$actual" >&2
        exit 1
    fi
}

if [[ ! -f "$proto_dir/probe/v1/probe.proto" || ! -f "$proto_dir/auth/v1/auth.proto" || ! -f "$proto_dir/config/v1/config.proto" || ! -f "$proto_dir/audit/v1/audit.proto" || ! -f "$proto_dir/warehouse/v1/warehouse.proto" ]]; then
    printf '缺少协议文件，请检查固定子模块版本，或使用 REALMMESH_PROTO_DIR 指定完整的本地协议工作树\n' >&2
    exit 1
fi

check_version "$(protoc --version)" "libprotoc 36.1"
check_version "$("$tool_dir/protoc-gen-go" --version)" "protoc-gen-go v1.36.11"
check_version "$("$tool_dir/protoc-gen-go-grpc" --version)" "protoc-gen-go-grpc 1.6.2"

mkdir -p gen

protoc \
    --proto_path="$proto_dir" \
    --plugin="protoc-gen-go=$tool_dir/protoc-gen-go" \
    --plugin="protoc-gen-go-grpc=$tool_dir/protoc-gen-go-grpc" \
    --go_out=gen \
    --go_opt=paths=source_relative \
    --go-grpc_out=gen \
    --go-grpc_opt=paths=source_relative \
    "$proto_dir/probe/v1/probe.proto" \
    "$proto_dir/auth/v1/auth.proto" \
    "$proto_dir/config/v1/config.proto" \
    "$proto_dir/audit/v1/audit.proto" \
    "$proto_dir/warehouse/v1/warehouse.proto"
