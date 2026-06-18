module github.com/xraph/nexus/_examples/grpc

go 1.25.7

require (
	github.com/xraph/nexus v0.0.0
	github.com/xraph/nexus/grpcsrv v0.0.0
	github.com/xraph/nexus/providers/openai v0.0.0
	google.golang.org/grpc v1.78.0
)

require (
	github.com/gofrs/uuid/v5 v5.3.2 // indirect
	github.com/xraph/go-utils v1.1.1 // indirect
	go.jetify.com/typeid/v2 v2.0.0-alpha.3 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace (
	github.com/xraph/nexus => ../..
	github.com/xraph/nexus/grpcsrv => ../../grpcsrv
	github.com/xraph/nexus/providers/openai => ../../providers/openai
)
