package anthropic

import (
	"bytes"
	"strings"
	"testing"
)

func TestScratchStreamError(t *testing.T) {
	start := "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_1\",\"model\":\"claude-opus-5-5\"}}\n\n"
	inputs := map[string]string{
		"error event": start + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"Overloaded\"}}\n\n",
		"cut off":     start + "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"Hel\"}}\n\n",
		"bad json":    start + "event: ping\ndata: {not json\n\n",
	}
	for name, input := range inputs {
		var buf bytes.Buffer
		err := translateStream(strings.NewReader(input), &buf, 1700000000, false)
		t.Logf("%s\nerr: %v\n%s", name, err, buf.String())
	}
}
