package athena

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseAthenaConfig_NormalizesBucketToOutputLocation(t *testing.T) {
	cfg, err := parseAthenaConfig("athena://?bucket=my-bucket&access_key_id=ak&secret_access_key=sk&region_name=us-east-1")
	require.NoError(t, err)
	require.Equal(t, "s3://my-bucket/", cfg.OutputLocation)
}

func TestParseAthenaConfig_NormalizesS3URIToOutputLocation(t *testing.T) {
	cfg, err := parseAthenaConfig("athena://?bucket=s3://my-bucket/prefix&access_key_id=ak&secret_access_key=sk&region_name=us-east-1")
	require.NoError(t, err)
	require.Equal(t, "s3://my-bucket/prefix/", cfg.OutputLocation)
}

func TestParseAthenaConfig_RequiresBucket(t *testing.T) {
	_, err := parseAthenaConfig("athena://?access_key_id=ak&secret_access_key=sk&region_name=us-east-1")
	require.Error(t, err)
}

func TestParseAthenaConfig_RequiresCredentialsOrProfile(t *testing.T) {
	_, err := parseAthenaConfig("athena://?bucket=my-bucket&region_name=us-east-1")
	require.Error(t, err)
}
