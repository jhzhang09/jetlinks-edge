package modbus

import (
	"errors"
	"testing"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestTypeMetaReturnsByteWidth(t *testing.T) {
	tests := []struct {
		typ  core.DataType
		want int
	}{
		{core.TypeInt16, 2},
		{core.TypeUInt32, 4},
		{core.TypeFloat64, 8},
	}
	for _, test := range tests {
		t.Run(string(test.typ), func(t *testing.T) {
			size, _, _, err := typeMeta(test.typ)
			if err != nil {
				t.Fatal(err)
			}
			if size != test.want {
				t.Fatalf("typeMeta(%s) size = %d, want %d", test.typ, size, test.want)
			}
		})
	}
}

func TestStatusReportsLastActivity(t *testing.T) {
	driver := &driverImpl{stats: map[string]int64{}}
	if status := driver.Status(); !status.LastTime.IsZero() {
		t.Fatalf("new driver LastTime = %s, want zero", status.LastTime)
	}

	driver.incErr(errors.New("read failed"))
	status := driver.Status()
	if status.LastError != "read failed" || status.LastTime.IsZero() {
		t.Fatalf("unexpected error status: %+v", status)
	}
	errorTime := status.LastTime

	time.Sleep(time.Millisecond)
	driver.incOK(3)
	status = driver.Status()
	if status.LastError != "" || !status.LastTime.After(errorTime) {
		t.Fatalf("unexpected success status: %+v", status)
	}
}
