package main

import (
	"testing"

	"github.com/project-eria/go-wot/protocolDnssd"
)

func TestNodeOf(t *testing.T) {
	for _, c := range []struct {
		entry protocolDnssd.Entry
		want  string
	}{
		{protocolDnssd.Entry{Host: "172-23-0-6.local.", TXT: protocolDnssd.TXT{Extra: map[string]string{"node": "rdc"}}}, "rdc"},
		{protocolDnssd.Entry{Host: "eria-aabbcc.local."}, "eria-aabbcc"},
		{protocolDnssd.Entry{Host: "172-23-0-7.local."}, ""},
	} {
		if got := nodeOf(c.entry); got != c.want {
			t.Errorf("nodeOf(%q) = %q, want %q", c.entry.Host, got, c.want)
		}
	}
}
