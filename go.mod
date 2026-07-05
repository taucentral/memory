module github.com/coevin/tau-plugins/memory

go 1.25.0

// tau is consumed as a third-party Go module dependency; no `replace`
// directive is used in the shipped code, matching the precedent set by
// plugins/headroom/go.mod, plugins/sdd/go.mod, and plugins/todos/go.mod.
//
// TEMPORARY: a replace directive is active below so this plugin compiles
// against the un-published SDK signature change (ObserveResponse now
// takes a streamErr error parameter). Once the new tau release is
// published, remove the replace line and bump the require to the new
// pseudo-version.

require github.com/coevin/tau v0.0.0-20260630093552-20cd8babf934

require (
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/bmatcuk/doublestar/v4 v4.10.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/fatih/color v1.13.0 // indirect
	github.com/gofrs/flock v0.13.0 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/go-hclog v1.6.3 // indirect
	github.com/hashicorp/go-plugin v1.8.0 // indirect
	github.com/hashicorp/yamux v0.1.2 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/mattn/go-colorable v0.1.12 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/oklog/run v1.1.0 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pkoukk/tiktoken-go v0.1.8 // indirect
	github.com/sabhiram/go-gitignore v0.0.0-20210923224102-525f6e181f06 // indirect
	github.com/sourcegraph/go-diff v0.8.0 // indirect
	go.etcd.io/bbolt v1.3.11 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260226221140-a57be14db171 // indirect
	google.golang.org/grpc v1.81.1 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/coevin/tau => ../../tau
