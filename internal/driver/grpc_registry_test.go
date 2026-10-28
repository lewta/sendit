package driver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/lewta/sendit/internal/config"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	reflectionpb "google.golang.org/grpc/reflection/grpc_reflection_v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

type scriptedReflectionServer struct {
	reflectionpb.UnimplementedServerReflectionServer
	descriptors [][]byte
}

func (s *scriptedReflectionServer) ServerReflectionInfo(stream grpc.BidiStreamingServer[reflectionpb.ServerReflectionRequest, reflectionpb.ServerReflectionResponse]) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	return stream.Send(&reflectionpb.ServerReflectionResponse{
		OriginalRequest: req,
		MessageResponse: &reflectionpb.ServerReflectionResponse_FileDescriptorResponse{
			FileDescriptorResponse: &reflectionpb.FileDescriptorResponse{
				FileDescriptorProto: s.descriptors,
			},
		},
	})
}

func startScriptedReflectionServer(t *testing.T, descriptors [][]byte) *grpc.ClientConn {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := grpc.NewServer()
	reflectionpb.RegisterServerReflectionServer(server, &scriptedReflectionServer{descriptors: descriptors})
	go server.Serve(listener) //nolint:errcheck
	t.Cleanup(func() {
		server.Stop()
		_ = listener.Close()
	})

	conn, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("create gRPC client: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func marshalLargeServiceDescriptor(t testing.TB, commentBytes int) []byte {
	t.Helper()
	requestName := "Request"
	responseName := "Response"
	serviceName := "Echo"
	methodName := "Call"
	inputType := ".oversize.Request"
	outputType := ".oversize.Response"
	descriptor := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("oversize.proto"),
		Package: proto.String("oversize"),
		Syntax:  proto.String("proto3"),
		MessageType: []*descriptorpb.DescriptorProto{
			{Name: &requestName},
			{Name: &responseName},
		},
		Service: []*descriptorpb.ServiceDescriptorProto{{
			Name: &serviceName,
			Method: []*descriptorpb.MethodDescriptorProto{{
				Name:       &methodName,
				InputType:  &inputType,
				OutputType: &outputType,
			}},
		}},
		SourceCodeInfo: &descriptorpb.SourceCodeInfo{
			Location: []*descriptorpb.SourceCodeInfo_Location{{
				Path:            []int32{6, 0},
				Span:            []int32{1, 0, 1, 1},
				LeadingComments: proto.String(strings.Repeat("x", commentBytes)),
			}},
		},
	}
	encoded, err := proto.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal large service descriptor: %v", err)
	}
	return encoded
}

type cancelAfterChecksContext struct {
	context.Context
	after  int64
	checks atomic.Int64
	done   chan struct{}
	once   sync.Once
}

func newCancelAfterChecksContext(after int64) *cancelAfterChecksContext {
	return &cancelAfterChecksContext{
		Context: context.Background(),
		after:   after,
		done:    make(chan struct{}),
	}
}

func (c *cancelAfterChecksContext) Done() <-chan struct{} { return c.done }

func (c *cancelAfterChecksContext) Err() error {
	if c.checks.Add(1) >= c.after {
		c.once.Do(func() { close(c.done) })
		return context.Canceled
	}
	return nil
}

func marshalFileDescriptor(t testing.TB, name string, dependencies ...string) []byte {
	t.Helper()
	descriptor := &descriptorpb.FileDescriptorProto{
		Name:       proto.String(name),
		Dependency: append([]string(nil), dependencies...),
		Syntax:     proto.String("proto3"),
	}
	encoded, err := proto.Marshal(descriptor)
	if err != nil {
		t.Fatalf("marshal descriptor %q: %v", name, err)
	}
	return encoded
}

func descriptorChain(t testing.TB, count int) [][]byte {
	t.Helper()
	result := make([][]byte, 0, count)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("chain/%04d.proto", i)
		if i == 0 {
			result = append(result, marshalFileDescriptor(t, name))
			continue
		}
		result = append(result, marshalFileDescriptor(t, name, fmt.Sprintf("chain/%04d.proto", i-1)))
	}
	return result
}

func TestBuildFileRegistryAcceptsShuffledDescriptors(t *testing.T) {
	root := marshalFileDescriptor(t, "root.proto")
	middle := marshalFileDescriptor(t, "middle.proto", "root.proto")
	leaf := marshalFileDescriptor(t, "leaf.proto", "middle.proto")

	registry, err := buildFileRegistry(context.Background(), [][]byte{leaf, root, middle}, grpcReflectionLimits{
		maxDescriptors:     3,
		maxBytes:           len(root) + len(middle) + len(leaf),
		maxDependencyDepth: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"root.proto", "middle.proto", "leaf.proto"} {
		if _, err := registry.FindFileByPath(name); err != nil {
			t.Errorf("descriptor %q not registered: %v", name, err)
		}
	}
}

func TestBuildFileRegistryDeduplicatesIdenticalDescriptors(t *testing.T) {
	descriptor := marshalFileDescriptor(t, "same.proto")
	registry, err := buildFileRegistry(context.Background(), [][]byte{descriptor, descriptor}, grpcReflectionLimits{
		maxDescriptors:     2,
		maxBytes:           2 * len(descriptor),
		maxDependencyDepth: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	registry.RangeFiles(func(protoreflect.FileDescriptor) bool {
		count++
		return true
	})
	if count != 1 {
		t.Fatalf("registered files = %d, want 1", count)
	}
}

func TestBuildFileRegistryRejectsConflictingDescriptorIdentity(t *testing.T) {
	first := marshalFileDescriptor(t, "same.proto")
	conflicting := &descriptorpb.FileDescriptorProto{
		Name:    proto.String("same.proto"),
		Package: proto.String("other"),
		Syntax:  proto.String("proto3"),
	}
	second, err := proto.Marshal(conflicting)
	if err != nil {
		t.Fatal(err)
	}

	_, err = buildFileRegistry(context.Background(), [][]byte{first, second}, grpcReflectionLimits{
		maxDescriptors:     2,
		maxBytes:           len(first) + len(second),
		maxDependencyDepth: 1,
	})
	if err == nil || !stringsContainAll(err.Error(), "conflicting", "same.proto") {
		t.Fatalf("error = %v, want conflicting descriptor identity", err)
	}
}

func TestBuildFileRegistryRejectsMissingAndCyclicDependencies(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		descriptor := marshalFileDescriptor(t, "child.proto", "missing.proto")
		_, err := buildFileRegistry(context.Background(), [][]byte{descriptor}, grpcReflectionLimits{
			maxDescriptors:     1,
			maxBytes:           len(descriptor),
			maxDependencyDepth: 1,
		})
		if err == nil || !stringsContainAll(err.Error(), "child.proto", "missing.proto") {
			t.Fatalf("error = %v, want missing dependency", err)
		}
	})

	t.Run("cycle", func(t *testing.T) {
		first := marshalFileDescriptor(t, "first.proto", "second.proto")
		second := marshalFileDescriptor(t, "second.proto", "first.proto")
		_, err := buildFileRegistry(context.Background(), [][]byte{first, second}, grpcReflectionLimits{
			maxDescriptors:     2,
			maxBytes:           len(first) + len(second),
			maxDependencyDepth: 2,
		})
		if err == nil || !stringsContainAll(err.Error(), "cyclic", "first.proto", "second.proto") {
			t.Fatalf("error = %v, want cyclic dependency", err)
		}
	})
}

func TestBuildFileRegistryEnforcesDescriptorLimits(t *testing.T) {
	chain := descriptorChain(t, 4)
	totalBytes := 0
	for _, descriptor := range chain {
		totalBytes += len(descriptor)
	}

	tests := []struct {
		name   string
		input  [][]byte
		limits grpcReflectionLimits
		want   string
	}{
		{
			name:  "descriptor count",
			input: chain,
			limits: grpcReflectionLimits{
				maxDescriptors:     3,
				maxBytes:           totalBytes,
				maxDependencyDepth: 4,
			},
			want: "descriptor count",
		},
		{
			name:  "aggregate bytes",
			input: chain,
			limits: grpcReflectionLimits{
				maxDescriptors:     4,
				maxBytes:           totalBytes - 1,
				maxDependencyDepth: 4,
			},
			want: "aggregate bytes",
		},
		{
			name:  "dependency depth",
			input: chain,
			limits: grpcReflectionLimits{
				maxDescriptors:     4,
				maxBytes:           totalBytes,
				maxDependencyDepth: 3,
			},
			want: "dependency depth",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := buildFileRegistry(context.Background(), test.input, test.limits)
			if err == nil || !stringsContainAll(err.Error(), test.want, "limit") {
				t.Fatalf("error = %v, want %q limit", err, test.want)
			}
		})
	}

	if _, err := buildFileRegistry(context.Background(), chain, grpcReflectionLimits{
		maxDescriptors:     len(chain),
		maxBytes:           totalBytes,
		maxDependencyDepth: len(chain),
	}); err != nil {
		t.Fatalf("maximum valid descriptor set rejected: %v", err)
	}
}

func TestBuildFileRegistryDefaultBoundaries(t *testing.T) {
	limits, err := reflectionLimitsForConfig(config.GRPCConfig{})
	if err != nil {
		t.Fatal(err)
	}
	maximumCount := make([][]byte, 0, limits.maxDescriptors)
	for i := 0; i < limits.maxDescriptors; i++ {
		maximumCount = append(maximumCount, marshalFileDescriptor(t, fmt.Sprintf("maximum/%04d.proto", i)))
	}
	if _, err := buildFileRegistry(context.Background(), maximumCount, limits); err != nil {
		t.Fatalf("maximum default descriptor count rejected: %v", err)
	}
	overCount := append(append([][]byte(nil), maximumCount...), marshalFileDescriptor(t, "maximum/overflow.proto"))
	if _, err := buildFileRegistry(context.Background(), overCount, limits); err == nil || !strings.Contains(err.Error(), "descriptor count limit") {
		t.Fatalf("over-count error = %v", err)
	}

	maximumDepth := descriptorChain(t, limits.maxDependencyDepth)
	if _, err := buildFileRegistry(context.Background(), maximumDepth, limits); err != nil {
		t.Fatalf("maximum default dependency depth rejected: %v", err)
	}
	overDepth := descriptorChain(t, limits.maxDependencyDepth+1)
	if _, err := buildFileRegistry(context.Background(), overDepth, limits); err == nil || !strings.Contains(err.Error(), "dependency depth limit") {
		t.Fatalf("over-depth error = %v", err)
	}
}

func TestBuildFileRegistryHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := buildFileRegistry(ctx, descriptorChain(t, 2), grpcReflectionLimits{
		maxDescriptors:     2,
		maxBytes:           1024,
		maxDependencyDepth: 2,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestBuildFileRegistryHonorsCancellationWithinDependencyWalk(t *testing.T) {
	const globalDependency = "google/protobuf/descriptor.proto"
	if _, err := protoregistry.GlobalFiles.FindFileByPath(globalDependency); err != nil {
		t.Fatalf("global dependency fixture unavailable: %v", err)
	}
	descriptor := marshalFileDescriptor(t, "fanout.proto",
		globalDependency,
		globalDependency,
		globalDependency,
		"missing.proto",
	)

	// Parsing and the descriptor-level graph pass consume two polls. Cancel on
	// the third dependency poll. Without inner-loop polling this fixture instead
	// reaches and reports the deliberately missing dependency.
	ctx := newCancelAfterChecksContext(5)
	_, err := buildFileRegistry(ctx, [][]byte{descriptor}, grpcReflectionLimits{
		maxDescriptors:     1,
		maxBytes:           len(descriptor),
		maxDependencyDepth: 1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled during dependency walk (checks = %d)", err, ctx.checks.Load())
	}
}

func TestFetchMethodInfoAllowsConfiguredReflectionResponseAboveDefaultGRPCLimit(t *testing.T) {
	descriptor := marshalLargeServiceDescriptor(t, 4<<20)
	if len(descriptor) <= 4<<20 {
		t.Fatalf("descriptor size = %d, want above default 4 MiB gRPC receive limit", len(descriptor))
	}
	conn := startScriptedReflectionServer(t, [][]byte{descriptor})
	limits := grpcReflectionLimits{
		maxDescriptors:     1,
		maxBytes:           len(descriptor),
		maxDependencyDepth: 1,
	}

	info, err := new(GRPCDriver).fetchMethodInfo(context.Background(), conn, "oversize.Echo", "/oversize.Echo/Call", limits)
	if err != nil {
		t.Fatalf("fetch method info for %d-byte descriptor: %v", len(descriptor), err)
	}
	if got := string(info.input.FullName()); got != "oversize.Request" {
		t.Fatalf("input descriptor = %q, want oversize.Request", got)
	}
}

func TestFetchMethodInfoRejectsReflectionResponseAboveConfiguredWireLimit(t *testing.T) {
	descriptor := marshalLargeServiceDescriptor(t, 2<<20)
	conn := startScriptedReflectionServer(t, [][]byte{descriptor})
	limits := grpcReflectionLimits{
		maxDescriptors:     1,
		maxBytes:           1 << 20,
		maxDependencyDepth: 1,
	}

	_, err := new(GRPCDriver).fetchMethodInfo(context.Background(), conn, "oversize.Echo", "/oversize.Echo/Call", limits)
	if err == nil || !stringsContainAll(err.Error(), "receiving reflection response", "ResourceExhausted") {
		t.Fatalf("error = %v, want transport ResourceExhausted above configured reflection wire limit", err)
	}
}

func BenchmarkBuildFileRegistryLinear(b *testing.B) {
	for _, count := range []int{64, 128, 256, 512} {
		b.Run(fmt.Sprintf("descriptors-%d", count), func(b *testing.B) {
			chain := descriptorChain(b, count)
			totalBytes := 0
			for _, descriptor := range chain {
				totalBytes += len(descriptor)
			}
			limits := grpcReflectionLimits{
				maxDescriptors:     count,
				maxBytes:           totalBytes,
				maxDependencyDepth: count,
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := buildFileRegistry(context.Background(), chain, limits); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func stringsContainAll(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if !strings.Contains(value, fragment) {
			return false
		}
	}
	return true
}
