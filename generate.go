//go:generate go run ./internal/tools/versiongen -android android/app/build.gradle.kts
// Codegen requires protoc, protoc-gen-go, and protoc-gen-go-grpc on PATH.
//go:generate protoc --go_out=. --go-grpc_out=. --go_opt=module=pkt.systems/centaurx --go-grpc_opt=module=pkt.systems/centaurx proto/runner/v1/runner.proto
//go:generate go run ./internal/tools/bootstrapgen -o . -force

package centaurx
