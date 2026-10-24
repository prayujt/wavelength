package rpc

import (
	"context"
	"errors"
	"io"

	"connectrpc.com/connect"
	pb "github.com/prayujt/wavelength/gen/wavelength/v1"
	"github.com/prayujt/wavelength/internal/auth"
)

func (s *service) GetPlaybackInfo(ctx context.Context, r *connect.Request[pb.GetPlaybackInfoRequest]) (*connect.Response[pb.GetPlaybackInfoResponse], error) {
	if r.Msg.Id == "" {
		return nil, invalid("id is required")
	}
	t, err := s.library.Get(ctx, r.Msg.Id)
	if err != nil {
		return nil, failure(err)
	}
	return connect.NewResponse(&pb.GetPlaybackInfoResponse{Track: track(t), HttpPath: "/media/tracks/" + t.ID, SupportsRanges: true}), nil
}
func (s *service) StreamAudio(ctx context.Context, r *connect.Request[pb.StreamAudioRequest], stream *connect.ServerStream[pb.StreamAudioResponse]) error {
	if err := auth.RequireScope(ctx, auth.ScopeStream); err != nil {
		return failure(err)
	}
	if r.Msg.Id == "" || r.Msg.OffsetBytes < 0 || r.Msg.LengthBytes < 0 {
		return invalid("id and nonnegative byte offset/length are required")
	}
	file, _, err := s.library.OpenTrack(ctx, r.Msg.Id)
	if err != nil {
		return failure(err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return failure(err)
	}
	if r.Msg.OffsetBytes > info.Size() {
		return connect.NewError(connect.CodeOutOfRange, errors.New("offset exceeds file size"))
	}
	if _, err = file.Seek(r.Msg.OffsetBytes, io.SeekStart); err != nil {
		return failure(err)
	}
	remaining := info.Size() - r.Msg.OffsetBytes
	if r.Msg.LengthBytes > 0 && r.Msg.LengthBytes < remaining {
		remaining = r.Msg.LengthBytes
	}
	offset := r.Msg.OffsetBytes
	buffer := make([]byte, 64<<10)
	for remaining > 0 {
		if err = ctx.Err(); err != nil {
			return failure(err)
		}
		chunk := int64(len(buffer))
		if remaining < chunk {
			chunk = remaining
		}
		n, err := io.ReadFull(file, buffer[:chunk])
		if err != nil {
			return failure(err)
		}
		if err = stream.Send(&pb.StreamAudioResponse{Data: append([]byte(nil), buffer[:n]...), OffsetBytes: offset}); err != nil {
			return err
		}
		offset += int64(n)
		remaining -= int64(n)
	}
	return nil
}
