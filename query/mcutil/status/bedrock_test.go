package status_test

import (
	"context"
	"testing"

	"goPinger/query/mcutil/status"
	"goPinger/query/mcutil/util"
)

func TestBedrock(t *testing.T) {
	resp, err := status.Bedrock(context.Background(), "lifesteal.net", util.DefaultBedrockPort)

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v\n", resp)
}
