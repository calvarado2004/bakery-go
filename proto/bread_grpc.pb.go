// Code generated by protoc-gen-go-grpc. DO NOT EDIT.
// versions:
// - protoc-gen-go-grpc v1.2.0
// - protoc             v4.23.3
// source: proto/bread.proto

package bread

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
// Requires gRPC-Go v1.32.0 or later.
const _ = grpc.SupportPackageIsVersion7

// MakeBreadClient is the client API for MakeBread service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type MakeBreadClient interface {
	BakeBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	SendBreadToBakery(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	MadeBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (MakeBread_MadeBreadStreamClient, error)
}

type makeBreadClient struct {
	cc grpc.ClientConnInterface
}

func NewMakeBreadClient(cc grpc.ClientConnInterface) MakeBreadClient {
	return &makeBreadClient{cc}
}

func (c *makeBreadClient) BakeBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.MakeBread/BakeBread", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *makeBreadClient) SendBreadToBakery(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.MakeBread/SendBreadToBakery", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *makeBreadClient) MadeBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (MakeBread_MadeBreadStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &MakeBread_ServiceDesc.Streams[0], "/bread.MakeBread/MadeBreadStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &makeBreadMadeBreadStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type MakeBread_MadeBreadStreamClient interface {
	Recv() (*BreadResponse, error)
	grpc.ClientStream
}

type makeBreadMadeBreadStreamClient struct {
	grpc.ClientStream
}

func (x *makeBreadMadeBreadStreamClient) Recv() (*BreadResponse, error) {
	m := new(BreadResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// MakeBreadServer is the server API for MakeBread service.
// All implementations must embed UnimplementedMakeBreadServer
// for forward compatibility
type MakeBreadServer interface {
	BakeBread(context.Context, *BreadRequest) (*BreadResponse, error)
	SendBreadToBakery(context.Context, *BreadRequest) (*BreadResponse, error)
	MadeBreadStream(*BreadRequest, MakeBread_MadeBreadStreamServer) error
	mustEmbedUnimplementedMakeBreadServer()
}

// UnimplementedMakeBreadServer must be embedded to have forward compatible implementations.
type UnimplementedMakeBreadServer struct {
}

func (UnimplementedMakeBreadServer) BakeBread(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BakeBread not implemented")
}
func (UnimplementedMakeBreadServer) SendBreadToBakery(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method SendBreadToBakery not implemented")
}
func (UnimplementedMakeBreadServer) MadeBreadStream(*BreadRequest, MakeBread_MadeBreadStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method MadeBreadStream not implemented")
}
func (UnimplementedMakeBreadServer) mustEmbedUnimplementedMakeBreadServer() {}

// UnsafeMakeBreadServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to MakeBreadServer will
// result in compilation errors.
type UnsafeMakeBreadServer interface {
	mustEmbedUnimplementedMakeBreadServer()
}

func RegisterMakeBreadServer(s grpc.ServiceRegistrar, srv MakeBreadServer) {
	s.RegisterService(&MakeBread_ServiceDesc, srv)
}

func _MakeBread_BakeBread_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MakeBreadServer).BakeBread(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.MakeBread/BakeBread",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MakeBreadServer).BakeBread(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MakeBread_SendBreadToBakery_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MakeBreadServer).SendBreadToBakery(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.MakeBread/SendBreadToBakery",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MakeBreadServer).SendBreadToBakery(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MakeBread_MadeBreadStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BreadRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(MakeBreadServer).MadeBreadStream(m, &makeBreadMadeBreadStreamServer{stream})
}

type MakeBread_MadeBreadStreamServer interface {
	Send(*BreadResponse) error
	grpc.ServerStream
}

type makeBreadMadeBreadStreamServer struct {
	grpc.ServerStream
}

func (x *makeBreadMadeBreadStreamServer) Send(m *BreadResponse) error {
	return x.ServerStream.SendMsg(m)
}

// MakeBread_ServiceDesc is the grpc.ServiceDesc for MakeBread service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var MakeBread_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.MakeBread",
	HandlerType: (*MakeBreadServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "BakeBread",
			Handler:    _MakeBread_BakeBread_Handler,
		},
		{
			MethodName: "SendBreadToBakery",
			Handler:    _MakeBread_SendBreadToBakery_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "MadeBreadStream",
			Handler:       _MakeBread_MadeBreadStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}

// CheckInventoryClient is the client API for CheckInventory service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type CheckInventoryClient interface {
	CheckBreadInventory(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	CheckBreadInventoryStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (CheckInventory_CheckBreadInventoryStreamClient, error)
}

type checkInventoryClient struct {
	cc grpc.ClientConnInterface
}

func NewCheckInventoryClient(cc grpc.ClientConnInterface) CheckInventoryClient {
	return &checkInventoryClient{cc}
}

func (c *checkInventoryClient) CheckBreadInventory(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.CheckInventory/CheckBreadInventory", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *checkInventoryClient) CheckBreadInventoryStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (CheckInventory_CheckBreadInventoryStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &CheckInventory_ServiceDesc.Streams[0], "/bread.CheckInventory/CheckBreadInventoryStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &checkInventoryCheckBreadInventoryStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type CheckInventory_CheckBreadInventoryStreamClient interface {
	Recv() (*BreadResponse, error)
	grpc.ClientStream
}

type checkInventoryCheckBreadInventoryStreamClient struct {
	grpc.ClientStream
}

func (x *checkInventoryCheckBreadInventoryStreamClient) Recv() (*BreadResponse, error) {
	m := new(BreadResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// CheckInventoryServer is the server API for CheckInventory service.
// All implementations must embed UnimplementedCheckInventoryServer
// for forward compatibility
type CheckInventoryServer interface {
	CheckBreadInventory(context.Context, *BreadRequest) (*BreadResponse, error)
	CheckBreadInventoryStream(*BreadRequest, CheckInventory_CheckBreadInventoryStreamServer) error
	mustEmbedUnimplementedCheckInventoryServer()
}

// UnimplementedCheckInventoryServer must be embedded to have forward compatible implementations.
type UnimplementedCheckInventoryServer struct {
}

func (UnimplementedCheckInventoryServer) CheckBreadInventory(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method CheckBreadInventory not implemented")
}
func (UnimplementedCheckInventoryServer) CheckBreadInventoryStream(*BreadRequest, CheckInventory_CheckBreadInventoryStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method CheckBreadInventoryStream not implemented")
}
func (UnimplementedCheckInventoryServer) mustEmbedUnimplementedCheckInventoryServer() {}

// UnsafeCheckInventoryServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to CheckInventoryServer will
// result in compilation errors.
type UnsafeCheckInventoryServer interface {
	mustEmbedUnimplementedCheckInventoryServer()
}

func RegisterCheckInventoryServer(s grpc.ServiceRegistrar, srv CheckInventoryServer) {
	s.RegisterService(&CheckInventory_ServiceDesc, srv)
}

func _CheckInventory_CheckBreadInventory_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CheckInventoryServer).CheckBreadInventory(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.CheckInventory/CheckBreadInventory",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CheckInventoryServer).CheckBreadInventory(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _CheckInventory_CheckBreadInventoryStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BreadRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(CheckInventoryServer).CheckBreadInventoryStream(m, &checkInventoryCheckBreadInventoryStreamServer{stream})
}

type CheckInventory_CheckBreadInventoryStreamServer interface {
	Send(*BreadResponse) error
	grpc.ServerStream
}

type checkInventoryCheckBreadInventoryStreamServer struct {
	grpc.ServerStream
}

func (x *checkInventoryCheckBreadInventoryStreamServer) Send(m *BreadResponse) error {
	return x.ServerStream.SendMsg(m)
}

// CheckInventory_ServiceDesc is the grpc.ServiceDesc for CheckInventory service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var CheckInventory_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.CheckInventory",
	HandlerType: (*CheckInventoryServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CheckBreadInventory",
			Handler:    _CheckInventory_CheckBreadInventory_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "CheckBreadInventoryStream",
			Handler:       _CheckInventory_CheckBreadInventoryStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}

// BuyBreadClient is the client API for BuyBread service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type BuyBreadClient interface {
	BuyBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	BuyBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (BuyBread_BuyBreadStreamClient, error)
}

type buyBreadClient struct {
	cc grpc.ClientConnInterface
}

func NewBuyBreadClient(cc grpc.ClientConnInterface) BuyBreadClient {
	return &buyBreadClient{cc}
}

func (c *buyBreadClient) BuyBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.BuyBread/BuyBread", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *buyBreadClient) BuyBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (BuyBread_BuyBreadStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &BuyBread_ServiceDesc.Streams[0], "/bread.BuyBread/BuyBreadStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &buyBreadBuyBreadStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type BuyBread_BuyBreadStreamClient interface {
	Recv() (*BreadResponse, error)
	grpc.ClientStream
}

type buyBreadBuyBreadStreamClient struct {
	grpc.ClientStream
}

func (x *buyBreadBuyBreadStreamClient) Recv() (*BreadResponse, error) {
	m := new(BreadResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// BuyBreadServer is the server API for BuyBread service.
// All implementations must embed UnimplementedBuyBreadServer
// for forward compatibility
type BuyBreadServer interface {
	BuyBread(context.Context, *BreadRequest) (*BreadResponse, error)
	BuyBreadStream(*BreadRequest, BuyBread_BuyBreadStreamServer) error
	mustEmbedUnimplementedBuyBreadServer()
}

// UnimplementedBuyBreadServer must be embedded to have forward compatible implementations.
type UnimplementedBuyBreadServer struct {
}

func (UnimplementedBuyBreadServer) BuyBread(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BuyBread not implemented")
}
func (UnimplementedBuyBreadServer) BuyBreadStream(*BreadRequest, BuyBread_BuyBreadStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method BuyBreadStream not implemented")
}
func (UnimplementedBuyBreadServer) mustEmbedUnimplementedBuyBreadServer() {}

// UnsafeBuyBreadServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to BuyBreadServer will
// result in compilation errors.
type UnsafeBuyBreadServer interface {
	mustEmbedUnimplementedBuyBreadServer()
}

func RegisterBuyBreadServer(s grpc.ServiceRegistrar, srv BuyBreadServer) {
	s.RegisterService(&BuyBread_ServiceDesc, srv)
}

func _BuyBread_BuyBread_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BuyBreadServer).BuyBread(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.BuyBread/BuyBread",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BuyBreadServer).BuyBread(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _BuyBread_BuyBreadStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BreadRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(BuyBreadServer).BuyBreadStream(m, &buyBreadBuyBreadStreamServer{stream})
}

type BuyBread_BuyBreadStreamServer interface {
	Send(*BreadResponse) error
	grpc.ServerStream
}

type buyBreadBuyBreadStreamServer struct {
	grpc.ServerStream
}

func (x *buyBreadBuyBreadStreamServer) Send(m *BreadResponse) error {
	return x.ServerStream.SendMsg(m)
}

// BuyBread_ServiceDesc is the grpc.ServiceDesc for BuyBread service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var BuyBread_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.BuyBread",
	HandlerType: (*BuyBreadServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "BuyBread",
			Handler:    _BuyBread_BuyBread_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "BuyBreadStream",
			Handler:       _BuyBread_BuyBreadStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}

// BuyOrderServiceClient is the client API for BuyOrderService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type BuyOrderServiceClient interface {
	BuyOrder(ctx context.Context, in *BuyOrderRequest, opts ...grpc.CallOption) (*BuyOrderResponse, error)
	BuyOrderStream(ctx context.Context, in *BuyOrderRequest, opts ...grpc.CallOption) (BuyOrderService_BuyOrderStreamClient, error)
}

type buyOrderServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewBuyOrderServiceClient(cc grpc.ClientConnInterface) BuyOrderServiceClient {
	return &buyOrderServiceClient{cc}
}

func (c *buyOrderServiceClient) BuyOrder(ctx context.Context, in *BuyOrderRequest, opts ...grpc.CallOption) (*BuyOrderResponse, error) {
	out := new(BuyOrderResponse)
	err := c.cc.Invoke(ctx, "/bread.BuyOrderService/BuyOrder", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *buyOrderServiceClient) BuyOrderStream(ctx context.Context, in *BuyOrderRequest, opts ...grpc.CallOption) (BuyOrderService_BuyOrderStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &BuyOrderService_ServiceDesc.Streams[0], "/bread.BuyOrderService/BuyOrderStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &buyOrderServiceBuyOrderStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type BuyOrderService_BuyOrderStreamClient interface {
	Recv() (*BuyOrderResponse, error)
	grpc.ClientStream
}

type buyOrderServiceBuyOrderStreamClient struct {
	grpc.ClientStream
}

func (x *buyOrderServiceBuyOrderStreamClient) Recv() (*BuyOrderResponse, error) {
	m := new(BuyOrderResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// BuyOrderServiceServer is the server API for BuyOrderService service.
// All implementations must embed UnimplementedBuyOrderServiceServer
// for forward compatibility
type BuyOrderServiceServer interface {
	BuyOrder(context.Context, *BuyOrderRequest) (*BuyOrderResponse, error)
	BuyOrderStream(*BuyOrderRequest, BuyOrderService_BuyOrderStreamServer) error
	mustEmbedUnimplementedBuyOrderServiceServer()
}

// UnimplementedBuyOrderServiceServer must be embedded to have forward compatible implementations.
type UnimplementedBuyOrderServiceServer struct {
}

func (UnimplementedBuyOrderServiceServer) BuyOrder(context.Context, *BuyOrderRequest) (*BuyOrderResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method BuyOrder not implemented")
}
func (UnimplementedBuyOrderServiceServer) BuyOrderStream(*BuyOrderRequest, BuyOrderService_BuyOrderStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method BuyOrderStream not implemented")
}
func (UnimplementedBuyOrderServiceServer) mustEmbedUnimplementedBuyOrderServiceServer() {}

// UnsafeBuyOrderServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to BuyOrderServiceServer will
// result in compilation errors.
type UnsafeBuyOrderServiceServer interface {
	mustEmbedUnimplementedBuyOrderServiceServer()
}

func RegisterBuyOrderServiceServer(s grpc.ServiceRegistrar, srv BuyOrderServiceServer) {
	s.RegisterService(&BuyOrderService_ServiceDesc, srv)
}

func _BuyOrderService_BuyOrder_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BuyOrderRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(BuyOrderServiceServer).BuyOrder(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.BuyOrderService/BuyOrder",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(BuyOrderServiceServer).BuyOrder(ctx, req.(*BuyOrderRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _BuyOrderService_BuyOrderStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BuyOrderRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(BuyOrderServiceServer).BuyOrderStream(m, &buyOrderServiceBuyOrderStreamServer{stream})
}

type BuyOrderService_BuyOrderStreamServer interface {
	Send(*BuyOrderResponse) error
	grpc.ServerStream
}

type buyOrderServiceBuyOrderStreamServer struct {
	grpc.ServerStream
}

func (x *buyOrderServiceBuyOrderStreamServer) Send(m *BuyOrderResponse) error {
	return x.ServerStream.SendMsg(m)
}

// BuyOrderService_ServiceDesc is the grpc.ServiceDesc for BuyOrderService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var BuyOrderService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.BuyOrderService",
	HandlerType: (*BuyOrderServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "BuyOrder",
			Handler:    _BuyOrderService_BuyOrder_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "BuyOrderStream",
			Handler:       _BuyOrderService_BuyOrderStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}

// RemoveOldBreadClient is the client API for RemoveOldBread service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type RemoveOldBreadClient interface {
	RemoveBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	RemoveBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (RemoveOldBread_RemoveBreadStreamClient, error)
}

type removeOldBreadClient struct {
	cc grpc.ClientConnInterface
}

func NewRemoveOldBreadClient(cc grpc.ClientConnInterface) RemoveOldBreadClient {
	return &removeOldBreadClient{cc}
}

func (c *removeOldBreadClient) RemoveBread(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.RemoveOldBread/RemoveBread", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *removeOldBreadClient) RemoveBreadStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (RemoveOldBread_RemoveBreadStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &RemoveOldBread_ServiceDesc.Streams[0], "/bread.RemoveOldBread/RemoveBreadStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &removeOldBreadRemoveBreadStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type RemoveOldBread_RemoveBreadStreamClient interface {
	Recv() (*BreadResponse, error)
	grpc.ClientStream
}

type removeOldBreadRemoveBreadStreamClient struct {
	grpc.ClientStream
}

func (x *removeOldBreadRemoveBreadStreamClient) Recv() (*BreadResponse, error) {
	m := new(BreadResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// RemoveOldBreadServer is the server API for RemoveOldBread service.
// All implementations must embed UnimplementedRemoveOldBreadServer
// for forward compatibility
type RemoveOldBreadServer interface {
	RemoveBread(context.Context, *BreadRequest) (*BreadResponse, error)
	RemoveBreadStream(*BreadRequest, RemoveOldBread_RemoveBreadStreamServer) error
	mustEmbedUnimplementedRemoveOldBreadServer()
}

// UnimplementedRemoveOldBreadServer must be embedded to have forward compatible implementations.
type UnimplementedRemoveOldBreadServer struct {
}

func (UnimplementedRemoveOldBreadServer) RemoveBread(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method RemoveBread not implemented")
}
func (UnimplementedRemoveOldBreadServer) RemoveBreadStream(*BreadRequest, RemoveOldBread_RemoveBreadStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method RemoveBreadStream not implemented")
}
func (UnimplementedRemoveOldBreadServer) mustEmbedUnimplementedRemoveOldBreadServer() {}

// UnsafeRemoveOldBreadServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to RemoveOldBreadServer will
// result in compilation errors.
type UnsafeRemoveOldBreadServer interface {
	mustEmbedUnimplementedRemoveOldBreadServer()
}

func RegisterRemoveOldBreadServer(s grpc.ServiceRegistrar, srv RemoveOldBreadServer) {
	s.RegisterService(&RemoveOldBread_ServiceDesc, srv)
}

func _RemoveOldBread_RemoveBread_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(RemoveOldBreadServer).RemoveBread(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.RemoveOldBread/RemoveBread",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(RemoveOldBreadServer).RemoveBread(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _RemoveOldBread_RemoveBreadStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BreadRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(RemoveOldBreadServer).RemoveBreadStream(m, &removeOldBreadRemoveBreadStreamServer{stream})
}

type RemoveOldBread_RemoveBreadStreamServer interface {
	Send(*BreadResponse) error
	grpc.ServerStream
}

type removeOldBreadRemoveBreadStreamServer struct {
	grpc.ServerStream
}

func (x *removeOldBreadRemoveBreadStreamServer) Send(m *BreadResponse) error {
	return x.ServerStream.SendMsg(m)
}

// RemoveOldBread_ServiceDesc is the grpc.ServiceDesc for RemoveOldBread service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var RemoveOldBread_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.RemoveOldBread",
	HandlerType: (*RemoveOldBreadServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "RemoveBread",
			Handler:    _RemoveOldBread_RemoveBread_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "RemoveBreadStream",
			Handler:       _RemoveOldBread_RemoveBreadStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}

// MakeOrderServiceClient is the client API for MakeOrderService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type MakeOrderServiceClient interface {
	MakeOrder(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error)
	MakeOrderStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (MakeOrderService_MakeOrderStreamClient, error)
}

type makeOrderServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewMakeOrderServiceClient(cc grpc.ClientConnInterface) MakeOrderServiceClient {
	return &makeOrderServiceClient{cc}
}

func (c *makeOrderServiceClient) MakeOrder(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (*BreadResponse, error) {
	out := new(BreadResponse)
	err := c.cc.Invoke(ctx, "/bread.MakeOrderService/MakeOrder", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *makeOrderServiceClient) MakeOrderStream(ctx context.Context, in *BreadRequest, opts ...grpc.CallOption) (MakeOrderService_MakeOrderStreamClient, error) {
	stream, err := c.cc.NewStream(ctx, &MakeOrderService_ServiceDesc.Streams[0], "/bread.MakeOrderService/MakeOrderStream", opts...)
	if err != nil {
		return nil, err
	}
	x := &makeOrderServiceMakeOrderStreamClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type MakeOrderService_MakeOrderStreamClient interface {
	Recv() (*BreadResponse, error)
	grpc.ClientStream
}

type makeOrderServiceMakeOrderStreamClient struct {
	grpc.ClientStream
}

func (x *makeOrderServiceMakeOrderStreamClient) Recv() (*BreadResponse, error) {
	m := new(BreadResponse)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// MakeOrderServiceServer is the server API for MakeOrderService service.
// All implementations must embed UnimplementedMakeOrderServiceServer
// for forward compatibility
type MakeOrderServiceServer interface {
	MakeOrder(context.Context, *BreadRequest) (*BreadResponse, error)
	MakeOrderStream(*BreadRequest, MakeOrderService_MakeOrderStreamServer) error
	mustEmbedUnimplementedMakeOrderServiceServer()
}

// UnimplementedMakeOrderServiceServer must be embedded to have forward compatible implementations.
type UnimplementedMakeOrderServiceServer struct {
}

func (UnimplementedMakeOrderServiceServer) MakeOrder(context.Context, *BreadRequest) (*BreadResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method MakeOrder not implemented")
}
func (UnimplementedMakeOrderServiceServer) MakeOrderStream(*BreadRequest, MakeOrderService_MakeOrderStreamServer) error {
	return status.Errorf(codes.Unimplemented, "method MakeOrderStream not implemented")
}
func (UnimplementedMakeOrderServiceServer) mustEmbedUnimplementedMakeOrderServiceServer() {}

// UnsafeMakeOrderServiceServer may be embedded to opt out of forward compatibility for this service.
// Use of this interface is not recommended, as added methods to MakeOrderServiceServer will
// result in compilation errors.
type UnsafeMakeOrderServiceServer interface {
	mustEmbedUnimplementedMakeOrderServiceServer()
}

func RegisterMakeOrderServiceServer(s grpc.ServiceRegistrar, srv MakeOrderServiceServer) {
	s.RegisterService(&MakeOrderService_ServiceDesc, srv)
}

func _MakeOrderService_MakeOrder_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(BreadRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(MakeOrderServiceServer).MakeOrder(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/bread.MakeOrderService/MakeOrder",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(MakeOrderServiceServer).MakeOrder(ctx, req.(*BreadRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func _MakeOrderService_MakeOrderStream_Handler(srv interface{}, stream grpc.ServerStream) error {
	m := new(BreadRequest)
	if err := stream.RecvMsg(m); err != nil {
		return err
	}
	return srv.(MakeOrderServiceServer).MakeOrderStream(m, &makeOrderServiceMakeOrderStreamServer{stream})
}

type MakeOrderService_MakeOrderStreamServer interface {
	Send(*BreadResponse) error
	grpc.ServerStream
}

type makeOrderServiceMakeOrderStreamServer struct {
	grpc.ServerStream
}

func (x *makeOrderServiceMakeOrderStreamServer) Send(m *BreadResponse) error {
	return x.ServerStream.SendMsg(m)
}

// MakeOrderService_ServiceDesc is the grpc.ServiceDesc for MakeOrderService service.
// It's only intended for direct use with grpc.RegisterService,
// and not to be introspected or modified (even as a copy)
var MakeOrderService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "bread.MakeOrderService",
	HandlerType: (*MakeOrderServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "MakeOrder",
			Handler:    _MakeOrderService_MakeOrder_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "MakeOrderStream",
			Handler:       _MakeOrderService_MakeOrderStream_Handler,
			ServerStreams: true,
		},
	},
	Metadata: "proto/bread.proto",
}
