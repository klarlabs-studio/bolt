module go.klarlabs.de/bolt/examples/microservices/http-middleware

go 1.26.0

toolchain go1.26.10

require (
	github.com/google/uuid v1.6.0
	go.klarlabs.de/bolt v1.7.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
)

// Local development - replace with actual module path in production
replace go.klarlabs.de/bolt => ../../..
