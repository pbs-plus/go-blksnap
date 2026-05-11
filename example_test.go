package blksnap_test

import (
	"fmt"

	"github.com/sralmerol/go-blksnap"
)

// ExampleUUID demonstrates UUID parsing and formatting.
func ExampleUUID() {
	id, err := blksnap.ParseUUID("550e8400-e29b-41d4-a716-446655440000")
	if err != nil {
		panic(err)
	}
	fmt.Println(id.String())
	// Output: 550e8400-e29b-41d4-a716-446655440000
}

// ExampleMustParseUUID demonstrates safe UUID parsing.
func ExampleMustParseUUID() {
	id := blksnap.MustParseUUID("550e8400-e29b-41d4-a716-446655440000")
	fmt.Println(id.IsZero())
	// Output: false
}

// ExampleVersion demonstrates version formatting.
func ExampleVersion() {
	v := blksnap.Version{Major: 1, Minor: 2, Revision: 3, Build: 4}
	fmt.Println(v.String())
	// Output: 1.2.3.4
}
