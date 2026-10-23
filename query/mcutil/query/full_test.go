package query_test

import (
	"context"
	"testing"

	"goPinger/query/mcutil/query"
	"goPinger/query/mcutil/util"
)

func TestFull(t *testing.T) {
	resp, err := query.Full(context.Background(), "demo.mcstatus.io", util.DefaultJavaPort)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v\n", resp)
}
