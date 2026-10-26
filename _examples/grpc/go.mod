module github.com/xraph/nexus/_examples/grpc

go 1.26.0

require (
	github.com/xraph/nexus v1.9.0
	github.com/xraph/nexus/grpcsrv v0.0.0
	github.com/xraph/nexus/providers/openai v0.0.0
	google.golang.org/grpc v1.83.2
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.14 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gofrs/uuid/v5 v5.3.2 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/xraph/go-utils v1.5.0 // indirect
	go.jetify.com/typeid/v2 v2.0.0-alpha.3 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.45.0 // indirect
	go.opentelemetry.io/otel/metric v1.45.0 // indirect
	go.opentelemetry.io/otel/trace v1.45.0 // indirect
	golang.org/x/net v0.60.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/term v0.46.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260803160001-6ac0973c030d // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
)

replace (
	github.com/xraph/nexus => ../..
	github.com/xraph/nexus/grpcsrv => ../../grpcsrv
	github.com/xraph/nexus/providers/openai => ../../providers/openai
)
