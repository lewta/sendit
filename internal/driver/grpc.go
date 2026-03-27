package driver

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"

	"github.com/lewta/sendit/internal/task"
)

// grpcStatusToHTTP maps a gRPC status code to an HTTP-like status code so the
// engine's error classifier and metrics work uniformly across all driver types.
//
//	OK(0)                 → 200
//	InvalidArgument(3)    → 400
//	OutOfRange(11)        → 400
//	Unauthenticated(16)   → 401
//	PermissionDenied(7)   → 403
//	NotFound(5)           → 404
//	AlreadyExists(6)      → 409
//	ResourceExhausted(8)  → 429
//	Unimplemented(12)     → 501
//	Unavailable(14)       → 503
//	DeadlineExceeded(4)   → 504
//	other                 → 500
//
// GRPCStatusToHTTP is exported for testing.
var GRPCStatusToHTTP = grpcStatusToHTTP

func grpcStatusToHTTP(c codes.Code) int {
	switch c {
	case codes.OK:
		return 200
	case codes.InvalidArgument, codes.OutOfRange:
		return 400
	case codes.Unauthenticated:
		return 401
	case codes.PermissionDenied:
		return 403
	case codes.NotFound:
		return 404
	case codes.AlreadyExists:
		return 409
	case codes.ResourceExhausted:
		return 429
	case codes.Unimplemented:
		return 501
	case codes.Unavailable:
		return 503
	case codes.DeadlineExceeded:
		return 504
	default:
		return 500
	}
}

type grpcMethodInfo struct {
	input  protoreflect.MessageDescriptor
	output protoreflect.MessageDescriptor
}

// GRPCDriver executes unary gRPC requests. It uses server reflection to resolve
// request/response types so no .proto files are required at runtime. Connections
// and method descriptors are cached across calls.
//
// URL format:
//
//	grpc://host:port/package.Service/Method   — plaintext
//	grpcs://host:port/package.Service/Method  — TLS
type GRPCDriver struct {
	mu      sync.Mutex
	conns   map[string]*grpc.ClientConn // keyed by addr+tlsMode
	methods sync.Map                    // keyed by addr+fullMethod → grpcMethodInfo
}

// NewGRPCDriver creates a GRPCDriver.
func NewGRPCDriver() *GRPCDriver {
	return &GRPCDriver{
		conns: make(map[string]*grpc.ClientConn),
	}
}

// Execute performs the unary gRPC call described by t.
func (d *GRPCDriver) Execute(ctx context.Context, t task.Task) task.Result {
	cfg := t.Config.GRPC

	u, err := url.Parse(t.URL)
	if err != nil {
		return task.Result{Task: t, Error: fmt.Errorf("parsing URL: %w", err)}
	}

	addr := u.Host
	fullMethod := u.Path // e.g. /package.Service/Method

	parts := strings.SplitN(strings.TrimPrefix(fullMethod, "/"), "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return task.Result{Task: t, Error: fmt.Errorf("grpc URL path must be /Service/Method, got %q", fullMethod)}
	}
	serviceName := parts[0]

	useTLS := u.Scheme == "grpcs" || cfg.TLS

	timeoutS := cfg.TimeoutS
	if timeoutS <= 0 {
		timeoutS = 15
	}

	callCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutS)*time.Second)
	defer cancel()

	conn, err := d.getConn(addr, useTLS, cfg.Insecure)
	if err != nil {
		return task.Result{Task: t, Error: err}
	}

	methodInfo, err := d.resolveMethod(callCtx, conn, serviceName, fullMethod)
	if err != nil {
		return task.Result{Task: t, Error: fmt.Errorf("resolving method via reflection: %w", err)}
	}

	reqMsg := dynamicpb.NewMessage(methodInfo.input)
	if cfg.Body != "" {
		if err := protojson.Unmarshal([]byte(cfg.Body), reqMsg); err != nil {
			return task.Result{Task: t, Error: fmt.Errorf("parsing body as JSON: %w", err)}
		}
	}

	respMsg := dynamicpb.NewMessage(methodInfo.output)

	start := time.Now()
	invokeErr := conn.Invoke(callCtx, fullMethod, reqMsg, respMsg)
	elapsed := time.Since(start)

	if invokeErr != nil {
		st, _ := status.FromError(invokeErr)
		return task.Result{
			Task:       t,
			StatusCode: grpcStatusToHTTP(st.Code()),
			Duration:   elapsed,
		}
	}

	return task.Result{
		Task:       t,
		StatusCode: 200,
		Duration:   elapsed,
	}
}

func (d *GRPCDriver) getConn(addr string, useTLS, insecureSkip bool) (*grpc.ClientConn, error) {
	tlsMode := "plain"
	if useTLS && insecureSkip {
		tlsMode = "tls-insecure"
	} else if useTLS {
		tlsMode = "tls"
	}
	key := addr + ":" + tlsMode

	d.mu.Lock()
	defer d.mu.Unlock()

	if conn, ok := d.conns[key]; ok {
		return conn, nil
	}

	var creds credentials.TransportCredentials
	if useTLS {
		if insecureSkip {
			creds = credentials.NewTLS(&tls.Config{InsecureSkipVerify: true}) //nolint:gosec // user-configured
		} else {
			creds = credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
		}
	} else {
		creds = insecure.NewCredentials()
	}

	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	if err != nil {
		return nil, fmt.Errorf("creating gRPC client for %q: %w", addr, err)
	}
	d.conns[key] = conn
	return conn, nil
}

func (d *GRPCDriver) resolveMethod(ctx context.Context, conn *grpc.ClientConn, serviceName, fullMethod string) (grpcMethodInfo, error) {
	cacheKey := conn.Target() + fullMethod
	if v, ok := d.methods.Load(cacheKey); ok {
		return v.(grpcMethodInfo), nil
	}

	info, err := d.fetchMethodInfo(ctx, conn, serviceName, fullMethod)
	if err != nil {
		return grpcMethodInfo{}, err
	}

	d.methods.Store(cacheKey, info)
	return info, nil
}

func (d *GRPCDriver) fetchMethodInfo(ctx context.Context, conn *grpc.ClientConn, serviceName, fullMethod string) (grpcMethodInfo, error) {
	rc := reflectionpb.NewServerReflectionClient(conn)
	stream, err := rc.ServerReflectionInfo(ctx)
	if err != nil {
		return grpcMethodInfo{}, fmt.Errorf("opening reflection stream: %w", err)
	}
	defer stream.CloseSend() //nolint:errcheck

	if err := stream.Send(&reflectionpb.ServerReflectionRequest{
		MessageRequest: &reflectionpb.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: serviceName,
		},
	}); err != nil {
		return grpcMethodInfo{}, fmt.Errorf("sending reflection request: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return grpcMethodInfo{}, fmt.Errorf("receiving reflection response: %w", err)
	}

	fdr, ok := resp.MessageResponse.(*reflectionpb.ServerReflectionResponse_FileDescriptorResponse)
	if !ok {
		if errResp, ok2 := resp.MessageResponse.(*reflectionpb.ServerReflectionResponse_ErrorResponse); ok2 {
			return grpcMethodInfo{}, fmt.Errorf("reflection error %d: %s",
				errResp.ErrorResponse.ErrorCode, errResp.ErrorResponse.ErrorMessage)
		}
		return grpcMethodInfo{}, fmt.Errorf("unexpected reflection response type: %T", resp.MessageResponse)
	}

	reg, err := buildFileRegistry(fdr.FileDescriptorResponse.FileDescriptorProto)
	if err != nil {
		return grpcMethodInfo{}, err
	}

	desc, err := reg.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return grpcMethodInfo{}, fmt.Errorf("service %q not found in reflection response: %w", serviceName, err)
	}

	sd, ok := desc.(protoreflect.ServiceDescriptor)
	if !ok {
		return grpcMethodInfo{}, fmt.Errorf("%q is not a service descriptor", serviceName)
	}

	methodName := protoreflect.Name(path.Base(fullMethod))
	md := sd.Methods().ByName(methodName)
	if md == nil {
		return grpcMethodInfo{}, fmt.Errorf("method %q not found in service %q", methodName, serviceName)
	}

	return grpcMethodInfo{
		input:  md.Input(),
		output: md.Output(),
	}, nil
}

// buildFileRegistry registers all FileDescriptorProtos returned by reflection
// into a local Files registry. Files are registered in dependency order via a
// retry loop (the reflection response may not be topologically sorted).
func buildFileRegistry(fdpBytes [][]byte) (*protoregistry.Files, error) {
	fdps := make([]*descriptorpb.FileDescriptorProto, 0, len(fdpBytes))
	for _, b := range fdpBytes {
		var fdp descriptorpb.FileDescriptorProto
		if err := proto.Unmarshal(b, &fdp); err != nil {
			return nil, fmt.Errorf("parsing FileDescriptorProto: %w", err)
		}
		fdps = append(fdps, &fdp)
	}

	reg := new(protoregistry.Files)
	resolver := &mergedResolver{local: reg}

	for attempt := 0; attempt <= len(fdps); attempt++ {
		var remaining []*descriptorpb.FileDescriptorProto
		for _, fdp := range fdps {
			if _, err := reg.FindFileByPath(fdp.GetName()); err == nil {
				continue // already registered
			}
			fd, err := protodesc.NewFile(fdp, resolver)
			if err != nil {
				remaining = append(remaining, fdp)
				continue
			}
			if err := reg.RegisterFile(fd); err != nil {
				remaining = append(remaining, fdp)
			}
		}
		if len(remaining) == 0 {
			break
		}
		fdps = remaining
	}

	return reg, nil
}

// mergedResolver resolves file descriptors from the local registry first,
// then falls back to the global registry (which contains well-known types).
type mergedResolver struct {
	local *protoregistry.Files
}

func (r *mergedResolver) FindFileByPath(p string) (protoreflect.FileDescriptor, error) {
	if fd, err := r.local.FindFileByPath(p); err == nil {
		return fd, nil
	}
	return protoregistry.GlobalFiles.FindFileByPath(p)
}

func (r *mergedResolver) FindDescriptorByName(name protoreflect.FullName) (protoreflect.Descriptor, error) {
	if d, err := r.local.FindDescriptorByName(name); err == nil {
		return d, nil
	}
	return protoregistry.GlobalFiles.FindDescriptorByName(name)
}
