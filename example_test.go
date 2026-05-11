package blksnap_test

import (
	"fmt"

	"github.com/pbs-plus/go-blksnap"
)

func ExampleUUID() {
	id, err := blksnap.ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		panic(err)
	}
	fmt.Println(id.String())
	// Output: 550e8400-e29b-41d4-a716-446655440000
}

func ExampleMustParseUUID() {
	id := blksnap.MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	fmt.Println(id.IsZero())
	// Output: false
}

func ExampleModuleVersion() {
	v := blksnap.ModuleVersion{Major: 1, Minor: 2, Revision: 3, Build: 4}
	fmt.Println(v.String())
	// Output: 1.2.3.4
}
