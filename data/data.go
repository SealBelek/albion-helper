package data

import _ "embed"

//go:embed items.json
var Items []byte

//go:embed world.json
var World []byte