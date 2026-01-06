# Code Generation

This module uses `protoc` for gRPC code generation.

## Requirements

- `protoc`
- `protoc-gen-go`
- `protoc-gen-go-grpc`

All tools must be available on `PATH`.

## Generate

From the repo root:

```sh
go generate ./...
```

This generates Go code from `proto/runner/v1/runner.proto` into
`internal/runnerpb` (see `option go_package` in the proto file).
