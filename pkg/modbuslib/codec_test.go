package modbuslib

import (
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)


func TestRequestRejectsMismatchedTransactionID(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	c := NewTCPClient("unused", 0, 1, time.Second, 0)
	c.conn = client
	go func() {
		request := make([]byte, 12)
		_, _ = io.ReadFull(server, request)
		response := []byte{0, 2, 0, 0, 0, 5, 1, 3, 2, 0, 1}
		_, _ = server.Write(response)
	}()

	if _, err := c.ReadHoldingRegisters(0, 1); err == nil {
		t.Fatal("expected mismatched transaction id to be rejected")
	}
}

func TestReadDiscreteInputsRejectsTruncatedBody(t *testing.T) {
	client, server := net.Pipe()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	c := NewTCPClient("unused", 0, 1, time.Second, 0)
	c.conn = client
	go func() {
		request := make([]byte, 12)
		_, _ = io.ReadFull(server, request)
		tid := binary.BigEndian.Uint16(request[:2])
		response := []byte{byte(tid >> 8), byte(tid), 0, 0, 0, 3, 1, 2, 2}
		_, _ = server.Write(response)
	}()

	if _, err := c.ReadDiscreteInputs(0, 1); err == nil {
		t.Fatal("expected truncated response to be rejected")
	}
}
