package daterange

import (
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	pb "github.com/notbbg/notbbg/server/pkg/protocol/notbbg/v1"
)

// GRPCAdapter bridges the generated DataServiceServer GetDataRange
// method to this package's Handler. Other DataService RPCs stay on
// UnimplementedDataServiceServer — Subscribe / Query / ExportData /
// GetFeedStatus aren't wired up yet (no gRPC listener exists today;
// the HTTP gateway serves these today over its own routes).
//
// To stand up a gRPC server later:
//
//	grpcSrv := grpc.NewServer()
//	pb.RegisterDataServiceServer(grpcSrv, daterange.NewGRPCAdapter(h))
//	grpcSrv.Serve(lis)
type GRPCAdapter struct {
	pb.UnimplementedDataServiceServer
	h *Handler
}

// NewGRPCAdapter wraps a Handler for use as a DataServiceServer.
func NewGRPCAdapter(h *Handler) *GRPCAdapter {
	return &GRPCAdapter{h: h}
}

// GetDataRange streams the proto-shaped chunks back to the gRPC
// client. Records are carried as Update messages with Topic set; the
// Handler's Record.Payload is opaque to gRPC today, so callers must
// decode the Topic to pick the right Update payload subtype.
func (a *GRPCAdapter) GetDataRange(req *pb.GetDataRangeRequest, stream grpc.ServerStreamingServer[pb.GetDataRangeResponse]) error {
	if req == nil {
		return fmt.Errorf("nil request")
	}
	if req.From == nil || req.To == nil {
		return fmt.Errorf("from and to required")
	}
	ir := Request{
		Topic:         req.Topic,
		From:          req.From.AsTime(),
		To:            req.To.AsTime(),
		Resolution:    req.Resolution,
		CorrelationID: req.CorrelationId,
		MaxRecords:    int(req.MaxRecords),
	}
	ctx := stream.Context()
	return a.h.Serve(ctx, ir, func(c Chunk) error {
		out := &pb.GetDataRangeResponse{
			CorrelationId: c.CorrelationID,
			Seq:           c.Seq,
			Eof:           c.EOF,
			Records:       updatesFromRecords(c.Records),
		}
		return stream.Send(out)
	})
}

func parseTimestamp(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, fmt.Errorf("empty")
	}
	return time.Parse(time.RFC3339Nano, s)
}

func updatesFromRecords(in []Record) []*pb.Update {
	out := make([]*pb.Update, 0, len(in))
	for _, r := range in {
		u := &pb.Update{Topic: r.Topic}
		// We only attach a typed timestamp when one is present on the
		// datalake record. The concrete payload subtype (OHLC, Trade,
		// LOB, News, Alert) depends on the topic and isn't reconstructed
		// here — clients that need structured payloads should decode
		// r.Payload using their own proto knowledge. This keeps the
		// adapter one-way and avoids replaying the topic-parse logic.
		if t, err := parseTimestamp(r.Timestamp); err == nil {
			u.Timestamp = timestamppb.New(t)
		}
		out = append(out, u)
	}
	return out
}
