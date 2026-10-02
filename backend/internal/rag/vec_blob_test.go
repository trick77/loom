package rag

import (
	"bytes"
	"testing"
)

func TestVecBlob_isLittleEndianFloat32(t *testing.T) {
	got := vecBlob([]float32{1, -2.5})
	want := []byte{0x00, 0x00, 0x80, 0x3f, 0x00, 0x00, 0x20, 0xc0}
	if !bytes.Equal(got, want) {
		t.Fatalf("vecBlob = % x, want % x", got, want)
	}
	if len(vecBlob(nil)) != 0 {
		t.Fatal("empty vector must encode to an empty blob")
	}
}
