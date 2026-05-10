module github.com/xraph/nexus/_examples/realtime

go 1.25.7

require (
	github.com/xraph/nexus v0.0.0
	github.com/xraph/nexus/providers/openairealtime v0.0.0
)

require github.com/coder/websocket v1.8.14 // indirect

replace (
	github.com/xraph/nexus => ../..
	github.com/xraph/nexus/providers/openairealtime => ../../providers/openairealtime
)
