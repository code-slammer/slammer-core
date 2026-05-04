package agentclient

import "testing"

func TestParseConnectOK(t *testing.T) {
	port, err := parseConnectOK("OK 1234\n")
	if err != nil {
		t.Fatal(err)
	}
	if port != 1234 {
		t.Fatalf("port = %d", port)
	}
}

func TestParseConnectOKRejectsBadResponses(t *testing.T) {
	for _, line := range []string{"", "ERR nope\n", "OK\n", "OK 0\n", "OK nope\n"} {
		if _, err := parseConnectOK(line); err == nil {
			t.Fatalf("expected error for %q", line)
		}
	}
}
