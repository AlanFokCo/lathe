module github.com/alanfokco/lathe

go 1.26.3

require (
	github.com/alanfokco/agentscope-go v0.0.0-00010101000000-000000000000
	github.com/spf13/cobra v1.10.2
)

require (
	github.com/google/uuid v1.6.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	golang.org/x/sys v0.46.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

// Dev-only: resolve agentscope-go from the local sibling checkout.
// Drop this replace (and use `go get github.com/alanfokco/agentscope-go@v2.0.3`)
// before publishing lathe, or once agentscope-go is fetchable from your proxy.
replace github.com/alanfokco/agentscope-go => ../agentscope-go
