# go2proto

Generate Protocol Buffer definitions from Go source code.

## Install

```bash
go install github.com/vinodhalaharvi/go2proto/cmd/go2proto@latest
```

Or build from source:

```bash
git clone https://github.com/vinodhalaharvi/go2proto.git
cd go2proto
go install ./cmd/go2proto
```

## Usage

```bash
# Current package
go2proto .

# All packages recursively
go2proto ./...

# Specific package
go2proto ./pkg/models

# With options
go2proto -out=./proto -package=myapp.v1 ./...
```

## Flags

| Flag | Description | Default |
|------|-------------|---------|
| `-out` | Output directory | `.` |
| `-package` | Proto package name | derived from Go package |
| `-go_package` | go_package option | Go import path |
| `-one-file` | Generate single .proto file | `false` |
| `-filename` | Output filename (with -one-file) | `generated.proto` |
| `-private` | Include unexported fields | `false` |
| `-v` | Verbose output | `false` |

## Comment Tags

Control generation with comment tags:

```go
// +go2proto=false
type Internal struct {}  // Skipped

// +go2proto:service
type UserService interface {  // Generates gRPC service
    GetUser(ctx context.Context, id string) (*User, error)
}
```

## Type Mappings

| Go | Proto |
|----|-------|
| `string` | `string` |
| `int`, `int64` | `int64` |
| `int32` | `int32` |
| `uint64` | `uint64` |
| `float32` | `float` |
| `float64` | `double` |
| `bool` | `bool` |
| `[]byte` | `bytes` |
| `[]T` | `repeated T` |
| `map[K]V` | `map<K, V>` |
| `*T` | `optional T` |
| `time.Time` | `google.protobuf.Timestamp` |
| `time.Duration` | `google.protobuf.Duration` |
| Generic type params | `google.protobuf.Any` |

## Example

Input (`models.go`):

```go
package models

type Status int

const (
    StatusUnknown Status = iota
    StatusActive
    StatusInactive
)

type User struct {
    ID        string
    Email     string
    Status    Status
    CreatedAt time.Time
}

// +go2proto:service
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) (*User, error)
}
```

Output (`models.proto`):

```protobuf
syntax = "proto3";

package models;

import "google/protobuf/timestamp.proto";

enum Status {
  STATUS_UNKNOWN = 0;
  STATUS_ACTIVE = 1;
  STATUS_INACTIVE = 2;
}

message User {
  string id = 1;
  string email = 2;
  Status status = 3;
  google.protobuf.Timestamp created_at = 4;
}

message GetUserRequest {
  string id = 1;
}

message CreateUserRequest {
  User user = 1;
}

service UserService {
  rpc GetUser(GetUserRequest) returns (User);
  rpc CreateUser(CreateUserRequest) returns (User);
}
```

## Workflow

```bash
# Generate protos from Go
go2proto -out=./proto ./...

# Generate code from protos (using buf)
buf generate ./proto
```

## Generics

Generic type parameters are mapped to `google.protobuf.Any`:

```go
type Result[T any] struct {
    Value T
    Error string
}
```

```protobuf
message Result {
  google.protobuf.Any value = 1;
  string error = 2;
}
```

## License

MIT
