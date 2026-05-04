package rootfsbuilder

import "testing"

func TestComputeChainID(t *testing.T) {
	got, err := ComputeChainID([]string{
		"sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = "sha256:4ab61be5d46a66e7f659b66144d4bead5b761c247b565fa202161590dcd9e45d"
	if got != want {
		t.Fatalf("chainID = %s, want %s", got, want)
	}
}

func TestComputeChainIDRejectsEmpty(t *testing.T) {
	if _, err := ComputeChainID(nil); err == nil {
		t.Fatal("expected error")
	}
}

func TestComputeChainIDRejectsUnsupportedDigest(t *testing.T) {
	if _, err := ComputeChainID([]string{"sha512:abc"}); err == nil {
		t.Fatal("expected error")
	}
}
