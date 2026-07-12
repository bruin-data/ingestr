package cmd

import (
	"bytes"
	"context"
	"net/url"
	"testing"

	_ "github.com/bruin-data/ingestr/pkg/destination/iceberg"
	"github.com/stretchr/testify/require"
)

func TestCheckCommandRunsIcebergWriteReadCleanup(t *testing.T) {
	var output bytes.Buffer
	app := NewApp()
	app.Writer = &output
	destURI := "iceberg+hadoop://?warehouse=" + url.QueryEscape(t.TempDir())

	require.NoError(t, app.Run(context.Background(), []string{
		"ingestr", "check", "--dest-uri", destURI,
	}))
	require.Contains(t, output.String(), "Destination connection check succeeded")
}
