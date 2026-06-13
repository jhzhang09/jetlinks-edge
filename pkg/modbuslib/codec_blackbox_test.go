package modbuslib_test

import (
	"testing"

	"github.com/jhzhang09/jetlinks-edge/pkg/modbuslib"
)

func TestParseAddressRejectsZeroOffset(t *testing.T) {
	if _, err := modbuslib.ParseAddress("40000"); err == nil {
		t.Fatal("expected zero offset address to be rejected")
	}
}

func TestAddressStringUsesFiveDigitPLCAddress(t *testing.T) {
	addr, err := modbuslib.ParseAddress("40001")
	if err != nil {
		t.Fatal(err)
	}
	if got := addr.String(); got != "40001" {
		t.Fatalf("Address.String() = %q, want %q", got, "40001")
	}
}

func TestEncodeBytesRejectsInvalidByteOrder(t *testing.T) {
	if _, err := modbuslib.EncodeBytes(uint32(1), "ZZZZ", true, false, 4); err == nil {
		t.Fatal("expected invalid byte order to be rejected")
	}
}
