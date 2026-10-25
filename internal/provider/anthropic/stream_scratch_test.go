package anthropic

import (
	"bytes"
	"os"
	"testing"
)

func TestScratchStream(t *testing.T) {
	f, err := os.Open("testdata/stream_text.sse")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	var buf bytes.Buffer
	if err := translateStream(f, &buf, 1700000000, true); err != nil {
		t.Fatal(err)
	}
	t.Log("\n" + buf.String())
}
