package searx

import (
	"context"
	"testing"

	"github.com/davecgh/go-spew/spew"
	"github.com/pkg/errors"
)

func TestClient(t *testing.T) {
	client := NewClient()

	ctx := context.Background()

	results, err := client.Search(ctx, "Cadoles site:linkedin.com !ddg !br !bi !go !qw")
	if err != nil {
		t.Fatalf("%+v", errors.WithStack(err))
	}

	spew.Dump(results)
}
