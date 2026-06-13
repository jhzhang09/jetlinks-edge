package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/jhzhang09/jetlinks-edge/internal/config"
	"github.com/jhzhang09/jetlinks-edge/internal/core"
	"github.com/jhzhang09/jetlinks-edge/internal/store"
)

func TestOperationsReturnsConfiguredButNotRunningGroupAlarm(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(config.StorageConfig{Driver: "sqlite", DSN: "file::memory:?cache=shared"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	group := &core.Group{ID: "group-1", Name: "PLC-1", Driver: "modbus-tcp", Enabled: true, IntervalMs: 1000}
	if err := st.SaveGroup(context.Background(), group); err != nil {
		t.Fatalf("save group: %v", err)
	}
	runner := core.NewRunner(core.NewDriverRegistry(), core.NewNorthRegistry(), st)
	handler := NewStatusHandler(runner, st)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/operations", nil)

	handler.Operations(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var response operationView
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Groups) != 1 || response.Groups[0].Running {
		t.Fatalf("groups = %#v", response.Groups)
	}
	if response.Runtime.NodeID == "" || response.Runtime.Goroutines <= 0 {
		t.Fatalf("runtime = %#v", response.Runtime)
	}
	if len(response.Alarms) != 1 || response.Alarms[0].SourceID != group.ID {
		t.Fatalf("alarms = %#v", response.Alarms)
	}
}
