package live

import (
	pb "github.com/Gyt-project/backend-api/pkg/grpc"
)

// GrpcClient is the shared gRPC client used by all live handlers to
// communicate with the main backend API.  It is set once at startup via
// InitGrpc and must be non-nil before any request is processed.
var GrpcClient pb.GytServiceClient

// InitGrpc injects the gRPC backend client into this package.
// Call this from cmd/live/main.go after the gRPC connection is established.
func InitGrpc(c pb.GytServiceClient) {
	GrpcClient = c
}
