module go.klarlabs.de/bolt/genai

go 1.25.0

require go.klarlabs.de/bolt v1.4.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
)

// Local development — pin to the in-tree bolt module. CI consumers
// override this via `go work` or by removing the directive in their
// own checkouts.
replace go.klarlabs.de/bolt => ../
