package status_test

import (
	"context"
	"testing"

	"goPinger/query/mcutil/status"
	"goPinger/query/mcutil/util"
)

func TestModern(t *testing.T) {
	resp, err := status.Modern(context.Background(), "demo.mcstatus.io", util.DefaultJavaPort)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v\n", resp)
}
