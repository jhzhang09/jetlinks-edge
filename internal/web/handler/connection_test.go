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

type secretTestDriver struct{}

func (secretTestDriver) Name() string                  { return "secret-driver" }
func (secretTestDriver) Connect(context.Context) error { return nil }
func (secretTestDriver) Disconnect() error             { return nil }
func (secretTestDriver) Status() core.DriverStatus     { return core.DriverStatus{} }

func TestConnectionResponsesRedactAndUpdatePreservesPassword(t *testing.T) {
	gin.SetMode(gin.TestMode)
	st, err := store.New(config.StorageConfig{Driver: "sqlite", DSN: "file:connection-handler-test?mode=memory&cache=shared"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Migrate(); err != nil {
		t.Fatal(err)
	}
	drivers := core.NewDriverRegistry()
	drivers.RegisterLifecycleExtension(core.ExtensionDescriptor{
		Type: "secret-driver", Name: "Secret Driver", Version: "1.0.0",
		ConnectionSchema: []core.ConfigField{
			{Key: "endpoint", Type: core.ConfigFieldText, Required: true},
			{Key: "password", Type: core.ConfigFieldPassword, Required: true},
		},
	}, func(context.Context, string, core.DriverConfig) (core.DriverLifecycle, error) {
		return secretTestDriver{}, nil
	})
	runner := core.NewRunner(drivers, core.NewNorthRegistry(), st)
	if err := runner.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(runner.Stop)
	handler := NewConnectionHandler(runner, st)

	created := invokeConnectionHandler(t, http.MethodPost, "/connections", "", `{
		"id":"conn-1","name":"Connection 1","driver":"secret-driver","enabled":false,
		"config":{"endpoint":"tcp://localhost","password":"original-secret"}
	}`, handler.Create)
	assertConnectionPassword(t, created, core.MaskedSecret)
	assertStoredConnectionPassword(t, st, "conn-1", "original-secret")

	updated := invokeConnectionHandler(t, http.MethodPut, "/connections/conn-1", "conn-1", `{
		"name":"Connection 1 updated","driver":"secret-driver","enabled":false,
		"config":{"endpoint":"tcp://example","password":"********"}
	}`, handler.Update)
	assertConnectionPassword(t, updated, core.MaskedSecret)
	assertStoredConnectionPassword(t, st, "conn-1", "original-secret")

	detail := invokeConnectionHandler(t, http.MethodGet, "/connections/conn-1", "conn-1", "", handler.Get)
	assertConnectionPassword(t, detail, core.MaskedSecret)
}

func invokeConnectionHandler(t *testing.T, method, path, id, body string, callback func(*gin.Context)) core.Connection {
	t.Helper()
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, path, bytes.NewBufferString(body))
	ctx.Request.Header.Set("Content-Type", "application/json")
	if id != "" {
		ctx.Params = gin.Params{{Key: "id", Value: id}}
	}
	callback(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("%s %s status = %d, body = %s", method, path, recorder.Code, recorder.Body.String())
	}
	var connection core.Connection
	if err := json.Unmarshal(recorder.Body.Bytes(), &connection); err != nil {
		t.Fatal(err)
	}
	return connection
}

func assertConnectionPassword(t *testing.T, connection core.Connection, expected string) {
	t.Helper()
	if password := connection.Config["password"]; password != expected {
		t.Fatalf("response password = %v, want %q", password, expected)
	}
}

func assertStoredConnectionPassword(t *testing.T, st *store.Store, id, expected string) {
	t.Helper()
	connection, err := st.GetConnection(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if password := connection.Config["password"]; password != expected {
		t.Fatalf("stored password = %v, want %q", password, expected)
	}
}
