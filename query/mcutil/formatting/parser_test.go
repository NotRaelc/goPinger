package formatting_test

import (
	"testing"

	"goPinger/query/mcutil/formatting"
)

func TestFormatting(t *testing.T) {
	res, err := formatting.Parse("\u00A7cT\u00A7ce\u00A7cs\u00A7ct")

	if err != nil {
		t.Fatal(err)
	}

	t.Logf("%+v\n", res.Tree)
}
