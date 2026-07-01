package psdbconnect

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	_ "google.golang.org/grpc/encoding/gzip" // register gzip so compressed Sync responses can be decoded
)

const connectSyncMethod = "/psdbconnect.v1alpha1.Connect/Sync"

// basicAuth carries PlanetScale credentials as an HTTP Basic credential. The
// psdbconnect endpoint accepts the database username/password (or, equivalently,
// a service token's name/value) as the Basic auth user/secret.
type basicAuth struct {
	header string
}

func newBasicAuth(name, value string) basicAuth {
	enc := base64.StdEncoding.EncodeToString([]byte(name + ":" + value))
	return basicAuth{header: "Basic " + enc}
}

func (a basicAuth) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": a.header}, nil
}

func (a basicAuth) RequireTransportSecurity() bool { return true }

// Client is a gRPC client for a PlanetScale psdbconnect endpoint.
type Client struct {
	cc *grpc.ClientConn
}

// Dial connects to a PlanetScale psdbconnect host (e.g. <db>.<org>.connect.psdb.cloud)
// over TLS, defaulting to port 443, authenticating with database credentials via
// HTTP Basic auth.
func Dial(host, username, password string) (*Client, error) {
	if host == "" {
		return nil, fmt.Errorf("psdbconnect: empty host")
	}
	target := host
	if _, _, err := net.SplitHostPort(host); err != nil {
		target = net.JoinHostPort(host, "443")
	}
	cc, err := grpc.NewClient(
		target,
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})),
		grpc.WithPerRPCCredentials(newBasicAuth(username, password)),
	)
	if err != nil {
		return nil, fmt.Errorf("psdbconnect: dial %s: %w", target, err)
	}
	return &Client{cc: cc}, nil
}

func (c *Client) Close() error {
	if c.cc == nil {
		return nil
	}
	return c.cc.Close()
}

// SyncStream is a server-streaming Sync response stream.
type SyncStream struct {
	grpc.ClientStream
}

func (s *SyncStream) Recv() (*SyncResponse, error) {
	m := new(SyncResponse)
	if err := s.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

// Sync opens the server-streaming Connect/Sync RPC for a single table cursor.
func (c *Client) Sync(ctx context.Context, req *SyncRequest) (*SyncStream, error) {
	stream, err := c.cc.NewStream(ctx, &grpc.StreamDesc{StreamName: "Sync", ServerStreams: true}, connectSyncMethod)
	if err != nil {
		return nil, err
	}
	if err := stream.SendMsg(req); err != nil {
		return nil, err
	}
	if err := stream.CloseSend(); err != nil {
		return nil, err
	}
	return &SyncStream{ClientStream: stream}, nil
}
