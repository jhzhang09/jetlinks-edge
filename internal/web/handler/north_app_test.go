package handler

import (
	"bytes"
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

const northHandlerTestType = "test-north"

type northHandlerTestApp struct{}

func (northHandlerTestApp) OnMessage(context.Context, core.NorthMessage) error { return nil }

func TestNorthAppResponsesRedactAndUpdatePreservesPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(config.StorageConfig{
		Driver: "sqlite",
		DSN:    "file:north-handler-test?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	north := core.NewNorthRegistry()
	north.RegisterMessageExtension(core.ExtensionDescriptor{
		Type:    northHandlerTestType,
		Name:    "Test North",
		Version: "1.0.0",
		ConfigSchema: []core.ConfigField{
			{Key: "broker", Type: core.ConfigFieldText, Required: true},
			{Key: "password", Type: core.ConfigFieldPassword, Required: true},
		},
	}, func(context.Context, string, core.NorthAppConfig) (core.NorthMessageHandler, error) {
		return northHandlerTestApp{}, nil
	})
	runner := core.NewRunner(core.NewDriverRegistry(), north, st)
	if err := runner.Start(context.Background()); err != nil {
		t.Fatalf("start runner: %v", err)
	}
	t.Cleanup(runner.Stop)
	h := NewNorthAppHandler(runner)

	created := invokeNorthAppHandler(t, http.MethodPost, "/north-apps", "", `{
		"id":"north-1","name":"North 1","type":"test-north","enabled":false,
		"config":{"broker":"tcp://localhost:1883","password":"original-secret"}
	}`, h.Create)
	assertNorthPassword(t, created, core.MaskedSecret)
	if created.Enabled {
		t.Fatal("disabled north app was unexpectedly enabled during creation")
	}
	assertStoredNorthPassword(t, st, "north-1", "original-secret")

	updated := invokeNorthAppHandler(t, http.MethodPut, "/north-apps/north-1", "north-1", `{
		"name":"North 1 updated","type":"test-north","enabled":false,
		"config":{"broker":"tcp://localhost:2883","password":"********"}
	}`, h.Update)
	assertNorthPassword(t, updated, core.MaskedSecret)
	assertStoredNorthPassword(t, st, "north-1", "original-secret")

	detail := invokeNorthAppHandler(t, http.MethodGet, "/north-apps/north-1", "north-1", "", h.Get)
	assertNorthPassword(t, detail, core.MaskedSecret)
}

func invokeNorthAppHandler(
	t *testing.T,
	method string,
	path string,
	id string,
	body string,
	handler func(*gin.Context),
) core.NorthApp {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	handler(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	var app core.NorthApp
	if err := json.Unmarshal(recorder.Body.Bytes(), &app); err != nil {
		t.Fatalf("decode %s %s response: %v", method, path, err)
	}
	return app
}

func assertNorthPassword(t *testing.T, app core.NorthApp, expected string) {
	t.Helper()
	if password := app.Config["password"]; password != expected {
		t.Fatalf("response password = %v, want %q", password, expected)
	}
}

func assertStoredNorthPassword(t *testing.T, st *store.Store, id string, expected string) {
	t.Helper()
	app, err := st.GetNorthApp(context.Background(), id)
	if err != nil {
		t.Fatalf("get stored north app: %v", err)
	}
	if app == nil {
		t.Fatal("stored north app is nil")
	}
	if password := app.Config["password"]; password != expected {
		t.Fatalf("stored password = %v, want %q", password, expected)
	}
}
