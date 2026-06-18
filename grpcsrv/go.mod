module github.com/xraph/nexus/grpcsrv

go 1.25.7

require (
	github.com/xraph/nexus v0.0.0
	google.golang.org/grpc v1.78.0
	google.golang.org/protobuf v1.36.11
)

require (
	go.opentelemetry.io/otel/sdk/metric v1.40.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260128011058-8636f8732409 // indirect
)

replace github.com/xraph/nexus => ../
