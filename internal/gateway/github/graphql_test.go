package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCompactGraphQL(t *testing.T) {
	assert.Equal(t,
		"query($id: ID!){node(id: $id){id}}",
		compactGraphQL(`
			query($id: ID!){
				node(id: $id){
					id
				}
			}
		`),
	)
}
