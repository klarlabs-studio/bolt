module go.klarlabs.de/bolt/examples/grpc-service

go 1.25.0

require (
	go.klarlabs.de/bolt v1.2.1
	go.opentelemetry.io/otel/trace v1.43.0
	google.golang.org/grpc v1.79.3
)

replace go.klarlabs.de/bolt => ../../

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.37.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251202230838-ff82c1b0f217 // indirect
	google.golang.org/protobuf v1.36.10 // indirect
)
