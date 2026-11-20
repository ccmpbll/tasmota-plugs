module github.com/ccmpbll/tasmota-plugs

go 1.26.11

require (
	github.com/prusa3d/prusa-connect/sdk v0.0.0
	gopkg.in/yaml.v3 v3.0.1
)

// The sdk is built from the checkout this module was written beside, so a
// change there is seen here without a release.
replace github.com/prusa3d/prusa-connect/sdk => ./.core/sdk
