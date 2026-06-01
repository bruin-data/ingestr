module github.com/bruin-data/ingestr

go 1.25.0

require (
	cloud.google.com/go/auth v0.18.2
	cloud.google.com/go/auth/oauth2adapt v0.2.8
	cloud.google.com/go/bigquery v1.72.0
	cloud.google.com/go/spanner v1.87.0
	cloud.google.com/go/storage v1.56.0
	github.com/Azure/azure-sdk-for-go/sdk/azcore v1.20.0
	github.com/Azure/azure-sdk-for-go/sdk/azidentity v1.13.1
	github.com/Azure/azure-sdk-for-go/sdk/storage/azblob v1.6.3
	github.com/Azure/azure-sdk-for-go/sdk/storage/azdatalake v1.4.4
	github.com/ClickHouse/clickhouse-go/v2 v2.42.0
	github.com/InfluxCommunity/influxdb3-go/v2 v2.13.0
	github.com/SAP/go-hdb v1.16.2
	github.com/alecthomas/chroma/v2 v2.23.1
	github.com/apache/arrow-adbc/go/adbc v1.9.0
	github.com/apache/arrow-go/v18 v18.5.1
	github.com/araddon/dateparse v0.0.0-20210429162001-6b43995a97de
	github.com/aws/aws-sdk-go-v2 v1.41.5
	github.com/aws/aws-sdk-go-v2/config v1.32.6
	github.com/aws/aws-sdk-go-v2/credentials v1.19.6
	github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue v1.20.37
	github.com/aws/aws-sdk-go-v2/service/athena v1.56.4
	github.com/aws/aws-sdk-go-v2/service/dynamodb v1.57.1
	github.com/aws/aws-sdk-go-v2/service/kinesis v1.43.4
	github.com/aws/aws-sdk-go-v2/service/s3 v1.88.1
	github.com/bmatcuk/doublestar/v4 v4.9.2
	github.com/columnar-tech/dbc v0.3.0
	github.com/couchbase/gocb/v2 v2.12.1
	github.com/databricks/databricks-sdk-go v0.95.0
	github.com/denisbrodbeck/machineid v1.0.1
	github.com/elastic/go-elasticsearch/v9 v9.3.1
	github.com/fatih/color v1.18.0
	github.com/go-chi/chi/v5 v5.2.3
	github.com/go-sql-driver/mysql v1.9.3
	github.com/golang-jwt/jwt/v5 v5.3.0
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/hamba/avro/v2 v2.31.0
	github.com/influxdata/influxdb-client-go/v2 v2.14.0
	github.com/jackc/pglogrepl v0.0.0-20251213150135-2e8d0df862c1
	github.com/jackc/pgx/v5 v5.7.6
	github.com/klauspost/compress v1.18.5
	github.com/mattn/go-sqlite3 v1.14.32
	github.com/microsoft/go-mssqldb v1.9.4
	github.com/muesli/termenv v0.16.0
	github.com/olekukonko/tablewriter v1.1.2
	github.com/pkg/sftp v1.13.10
	github.com/rabbitmq/amqp091-go v1.10.0
	github.com/schollz/progressbar/v3 v3.18.0
	github.com/segmentio/kafka-go v0.4.50
	github.com/shenzhencenter/google-ads-pb v1.23.0
	github.com/shirou/gopsutil/v4 v4.26.3
	github.com/shopspring/decimal v1.4.0
	github.com/sijms/go-ora/v2 v2.9.0
	github.com/simpleforce/simpleforce v0.0.0-20220429021116-acf4ac67ef68
	github.com/snowflakedb/gosnowflake v1.18.1
	github.com/sourcegraph/conc v0.3.0
	github.com/stretchr/testify v1.11.1
	github.com/stripe/stripe-go/v81 v81.4.0
	github.com/testcontainers/testcontainers-go v0.41.0
	github.com/testcontainers/testcontainers-go/modules/clickhouse v0.40.0
	github.com/testcontainers/testcontainers-go/modules/mongodb v0.41.0
	github.com/testcontainers/testcontainers-go/modules/mssql v0.40.0
	github.com/testcontainers/testcontainers-go/modules/mysql v0.40.0
	github.com/testcontainers/testcontainers-go/modules/postgres v0.40.0
	github.com/testcontainers/testcontainers-go/modules/rabbitmq v0.41.0
	github.com/trinodb/trino-go-client v0.333.0
	github.com/urfave/cli/v3 v3.0.0-beta1
	github.com/youmark/pkcs8 v0.0.0-20240726163527-a2c0da244d78
	go.mongodb.org/mongo-driver v1.17.6
	golang.org/x/crypto v0.48.0
	golang.org/x/exp v0.0.0-20251209150349-8475f28825e9
	golang.org/x/net v0.51.0
	golang.org/x/oauth2 v0.36.0
	golang.org/x/term v0.40.0
	golang.org/x/text v0.35.0
	golang.org/x/time v0.15.0
	google.golang.org/api v0.271.0
	google.golang.org/grpc v1.79.3
	google.golang.org/protobuf v1.36.11
	modernc.org/sqlite v1.40.1
	resty.dev/v3 v3.0.0-beta.5
)

require (
	cel.dev/expr v0.25.1 // indirect
	cloud.google.com/go v0.123.0 // indirect
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	cloud.google.com/go/iam v1.5.3 // indirect
	cloud.google.com/go/longrunning v0.8.0 // indirect
	cloud.google.com/go/monitoring v1.24.3 // indirect
	dario.cat/mergo v1.0.2 // indirect
	filippo.io/edwards25519 v1.1.0 // indirect
	github.com/99designs/go-keychain v0.0.0-20191008050251-8e49817e8af4 // indirect
	github.com/99designs/keyring v1.2.2 // indirect
	github.com/Azure/azure-sdk-for-go/sdk/internal v1.11.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/AzureAD/microsoft-authentication-library-for-go v1.6.0 // indirect
	github.com/BurntSushi/toml v1.4.0 // indirect
	github.com/ClickHouse/ch-go v0.69.0 // indirect
	github.com/GoogleCloudPlatform/grpc-gcp-go/grpcgcp v1.5.3 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/detectors/gcp v1.30.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/exporter/metric v0.53.0 // indirect
	github.com/GoogleCloudPlatform/opentelemetry-operations-go/internal/resourcemapping v0.53.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/ProtonMail/go-crypto v1.3.0 // indirect
	github.com/ProtonMail/gopenpgp/v3 v3.3.0 // indirect
	github.com/andybalholm/brotli v1.2.0 // indirect
	github.com/apache/arrow/go/v15 v15.0.2 // indirect
	github.com/apache/thrift v0.22.0 // indirect
	github.com/apapsch/go-jsonmerge/v2 v2.0.0 // indirect
	github.com/aws/aws-sdk-go-v2/aws/protocol/eventstream v1.7.8 // indirect
	github.com/aws/aws-sdk-go-v2/feature/ec2/imds v1.18.16 // indirect
	github.com/aws/aws-sdk-go-v2/feature/s3/manager v1.17.11 // indirect
	github.com/aws/aws-sdk-go-v2/internal/configsources v1.4.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/endpoints/v2 v2.7.21 // indirect
	github.com/aws/aws-sdk-go-v2/internal/ini v1.8.4 // indirect
	github.com/aws/aws-sdk-go-v2/internal/v4a v1.4.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/dynamodbstreams v1.32.14 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/accept-encoding v1.13.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/checksum v1.8.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/endpoint-discovery v1.11.21 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/presigned-url v1.13.16 // indirect
	github.com/aws/aws-sdk-go-v2/service/internal/s3shared v1.19.7 // indirect
	github.com/aws/aws-sdk-go-v2/service/signin v1.0.4 // indirect
	github.com/aws/aws-sdk-go-v2/service/sso v1.30.8 // indirect
	github.com/aws/aws-sdk-go-v2/service/ssooidc v1.35.12 // indirect
	github.com/aws/aws-sdk-go-v2/service/sts v1.41.5 // indirect
	github.com/aws/smithy-go v1.24.2 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/cloudflare/circl v1.6.3 // indirect
	github.com/cncf/xds/go v0.0.0-20251210132809-ee656c7534f5 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/couchbase/gocbcore/v10 v10.9.1 // indirect
	github.com/couchbase/gocbcoreps v0.1.5-0.20260107140814-1c3a03f888f8 // indirect
	github.com/couchbase/goprotostellar v1.0.5 // indirect
	github.com/couchbaselabs/gocbconnstr/v2 v2.0.0 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/creack/pty v1.1.24 // indirect
	github.com/danieljoos/wincred v1.2.2 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/docker/docker v28.5.2+incompatible // indirect
	github.com/docker/go-connections v0.6.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/dvsekhvalnov/jose2go v1.7.0 // indirect
	github.com/ebitengine/purego v0.10.0 // indirect
	github.com/elastic/elastic-transport-go/v8 v8.8.0 // indirect
	github.com/envoyproxy/go-control-plane/envoy v1.36.0 // indirect
	github.com/envoyproxy/protoc-gen-validate v1.3.0 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/gabriel-vasile/mimetype v1.4.7 // indirect
	github.com/go-faster/city v1.0.1 // indirect
	github.com/go-faster/errors v0.7.1 // indirect
	github.com/go-faster/jx v1.2.0 // indirect
	github.com/go-faster/yaml v0.4.6 // indirect
	github.com/go-jose/go-jose/v4 v4.1.3 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.4.0 // indirect
	github.com/goccy/go-json v0.10.5 // indirect
	github.com/godbus/dbus v0.0.0-20190726142602-4481cbc300e2 // indirect
	github.com/golang-sql/civil v0.0.0-20220223132316-b832511892a9 // indirect
	github.com/golang-sql/sqlexp v0.1.0 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/snappy v1.0.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/go-querystring v1.1.0 // indirect
	github.com/google/s2a-go v0.1.9 // indirect
	github.com/googleapis/enterprise-certificate-proxy v0.3.14 // indirect
	github.com/googleapis/gax-go/v2 v2.17.0 // indirect
	github.com/gsterjov/go-libsecret v0.0.0-20161001094733-a6f4afe4910c // indirect
	github.com/hashicorp/go-uuid v1.0.3 // indirect
	github.com/influxdata/line-protocol v0.0.0-20200327222509-2487e7298839 // indirect
	github.com/influxdata/line-protocol/v2 v2.2.1 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/jcmturner/aescts/v2 v2.0.0 // indirect
	github.com/jcmturner/dnsutils/v2 v2.0.0 // indirect
	github.com/jcmturner/gofork v1.7.6 // indirect
	github.com/jcmturner/goidentity/v6 v6.0.1 // indirect
	github.com/jcmturner/gokrb5/v8 v8.4.4 // indirect
	github.com/jcmturner/rpc/v2 v2.0.3 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/asmfmt v1.3.2 // indirect
	github.com/klauspost/cpuid/v2 v2.3.0 // indirect
	github.com/kr/fs v0.1.0 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/lucasb-eyer/go-colorful v1.3.0 // indirect
	github.com/lufia/plan9stats v0.0.0-20250317134145-8bc96cf8fc35 // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.20 // indirect
	github.com/minio/asm2plan9s v0.0.0-20200509001527-cdd76441f9d8 // indirect
	github.com/minio/c2goasm v0.0.0-20190812172519-36a3d3bbc4f3 // indirect
	github.com/mitchellh/colorstring v0.0.0-20190213212951-d06e56a500db // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.2.0 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.6.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/montanaflynn/stats v0.7.1 // indirect
	github.com/morikuni/aec v1.0.0 // indirect
	github.com/mtibben/percent v0.2.1 // indirect
	github.com/ncruces/go-strftime v0.1.9 // indirect
	github.com/oapi-codegen/runtime v1.0.0 // indirect
	github.com/olekukonko/cat v0.0.0-20250911104152-50322a0618f6 // indirect
	github.com/olekukonko/errors v1.1.0 // indirect
	github.com/olekukonko/ll v0.1.3 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/paulmach/orb v0.12.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/pierrec/lz4 v2.6.1+incompatible // indirect
	github.com/pierrec/lz4/v4 v4.1.23 // indirect
	github.com/pkg/browser v0.0.0-20240102092130-5ac0b6a4141c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/planetscale/vtprotobuf v0.6.1-0.20240319094008-0393e58bdf10 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/segmentio/asm v1.2.1 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spiffe/go-spiffe/v2 v2.6.0 // indirect
	github.com/tklauser/go-sysconf v0.3.16 // indirect
	github.com/tklauser/numcpus v0.11.0 // indirect
	github.com/xdg-go/pbkdf2 v1.0.0 // indirect
	github.com/xdg-go/scram v1.1.2 // indirect
	github.com/xdg-go/stringprep v1.0.4 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	github.com/zeebo/xxh3 v1.0.2 // indirect
	github.com/zeroshade/machine-id v0.0.0-20251223181436-930511047eef // indirect
	go.opencensus.io v0.24.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/detectors/gcp v1.39.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.65.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.41.0 // indirect
	go.opentelemetry.io/otel/metric v1.41.0 // indirect
	go.opentelemetry.io/otel/sdk v1.41.0 // indirect
	go.opentelemetry.io/otel/sdk/metric v1.41.0 // indirect
	go.opentelemetry.io/otel/trace v1.41.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/mod v0.33.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/telemetry v0.0.0-20260209163413-e7419c687ee4 // indirect
	golang.org/x/tools v0.42.0 // indirect
	golang.org/x/xerrors v0.0.0-20240903120638-7835f813f4da // indirect
	google.golang.org/genproto v0.0.0-20260203192932-546029d2fa20 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260203192932-546029d2fa20 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	gopkg.in/ini.v1 v1.67.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.66.10 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
