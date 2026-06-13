package modbus

import (
	"context"
	"testing"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestReadTagsMarksInvalidAddressBad(t *testing.T) {
	driver := &driverImpl{stats: map[string]int64{}}
	values, err := driver.ReadTags(context.Background(), []core.Tag{{
		ID:      "tag-1",
		Name:    "invalid",
		Address: "bad",
		Type:    core.TypeUInt16,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Quality != core.QualityBad {
		t.Fatalf("values = %+v, want one bad value", values)
	}
}
