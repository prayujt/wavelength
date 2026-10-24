package rpc

import (
	"context"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
)

func (s *service) StartScan(ctx context.Context, r *connect.Request[pb.StartScanRequest]) (*connect.Response[pb.StartScanResponse], error) {
	result, err := s.library.Scan(ctx)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.StartScanResponse{Indexed: int32(result.Indexed), MetadataWarnings: int32(result.MetadataWarnings)}), nil
}
func (s *service) GetScanStatus(ctx context.Context, r *connect.Request[pb.GetScanStatusRequest]) (*connect.Response[pb.GetScanStatusResponse], error) {
	scanning, count := s.library.ScanStatus()
	return connect.NewResponse(&pb.GetScanStatusResponse{Scanning: scanning, Indexed: count}), nil
}
