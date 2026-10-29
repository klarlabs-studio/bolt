module go.klarlabs.de/bolt/benchmarks

go 1.26.0

toolchain go1.26.10

require (
	github.com/rs/zerolog v1.34.0
	go.klarlabs.de/bolt v0.0.0-00010101000000-000000000000
	go.uber.org/zap v1.27.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.19 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/sys v0.44.0 // indirect
)

// Use local parent module for development
replace go.klarlabs.de/bolt => ../
