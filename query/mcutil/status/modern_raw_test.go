package status_test

import (
	"context"
	"testing"

	"goPinger/query/mcutil/status"
	"goPinger/query/mcutil/util"
)

func TestModernRaw(t *testing.T) {
	resp, err := status.ModernRaw(context.Background(), "demo.mcstatus.io", util.DefaultJavaPort)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v\n", resp)
}
