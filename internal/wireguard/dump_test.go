package wireguard

import (
	"testing"
	"time"
)

func TestParseDump(t *testing.T) {
	out := "cHJpdmF0ZQ==\tcHVibGlj\t0\toff\n" +
		"xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\t(none)\t203.0.113.5:51820\t0.0.0.0/0,::/0\t1790000000\t1500\t2600\t25\n"
	s, ok := ParseDump(out)
	if !ok || s.Endpoint != "203.0.113.5:51820" || s.ReceivedBytes != 1500 || s.SentBytes != 2600 {
		t.Fatalf("got %+v, %v", s, ok)
	}
	if !s.LatestHandshake.Equal(time.Unix(1790000000, 0)) {
		t.Errorf("handshake = %v", s.LatestHandshake)
	}

	never, _ := ParseDump("x\ty\t0\toff\npk\t(none)\t(none)\t0.0.0.0/0\t0\t0\t0\t25\n")
	if !never.LatestHandshake.IsZero() || never.Endpoint != "" {
		t.Errorf("no handshake yet: got %+v", never)
	}
	if _, ok := ParseDump(""); ok {
		t.Error("empty output must not parse")
	}
}
