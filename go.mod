module github.com/ubuntu/grubmenugen-zfs-tests

go 1.12

require (
	github.com/bicomsystems/go-libzfs v0.2.3
	github.com/otiai10/copy v1.0.1
	github.com/otiai10/curr v0.0.0-20150429015615-9b4961190c95 // indirect
	github.com/stretchr/testify v1.3.0
	gopkg.in/yaml.v2 v2.2.2
)

// Check https://github.com/bicomsystems/go-libzfs/pull/20
replace github.com/bicomsystems/go-libzfs => github.com/ubuntu/go-libzfs v0.2.2-0.20190715120733-dfe7058c3c21
