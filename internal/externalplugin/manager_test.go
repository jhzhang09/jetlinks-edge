package externalplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

func TestManagerHotAddsUpdatesAndRemovesPlugin(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "example-plugin")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0750); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(directory, "example.json")
	writeManifest := func(version string) {
		t.Helper()
		content := `{
  "apiVersion": "jetlinks-edge-plugin/v1",
  "kind": "driver",
  "command": "./example-plugin",
  "descriptor": {
    "type": "example-driver",
    "name": "Example Driver",
    "version": "` + version + `",
    "capabilities": ["polling"]
  }
}`
		if err := os.WriteFile(manifestPath, []byte(content), 0640); err != nil {
			t.Fatal(err)
		}
	}

	drivers := core.NewDriverRegistry()
	north := core.NewNorthRegistry()
	manager := NewManager(directory, drivers, north)

	writeManifest("1.0.0")
	changes, err := manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.DriverTypes) != 1 || changes.DriverTypes[0] != "example-driver" {
		t.Fatalf("unexpected add changes: %#v", changes)
	}
	if descriptor, ok := drivers.Descriptor("example-driver"); !ok || descriptor.Runtime != "external-process" {
		t.Fatalf("external descriptor not registered: %#v %v", descriptor, ok)
	}

	writeManifest("1.1.0")
	changes, err = manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.DriverTypes) != 1 {
		t.Fatalf("plugin update not detected: %#v", changes)
	}

	if err := os.Remove(manifestPath); err != nil {
		t.Fatal(err)
	}
	changes, err = manager.Reload()
	if err != nil {
		t.Fatal(err)
	}
	if len(changes.RemovedDriverTypes) != 1 || changes.RemovedDriverTypes[0] != "example-driver" {
		t.Fatalf("unexpected remove changes: %#v", changes)
	}
	if _, ok := drivers.Descriptor("example-driver"); ok {
		t.Fatal("removed plugin is still registered")
	}
}

func TestEmptyChangeSetSerializesArrays(t *testing.T) {
	changes := diffPlugins(map[string]Manifest{}, map[string]Manifest{})
	data, err := json.Marshal(changes)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"driverTypes":[],"northTypes":[],"removedDriverTypes":[],"removedNorthTypes":[]}` {
		t.Fatalf("unexpected empty changes JSON: %s", data)
	}
}

func TestExternalDriverProcessProtocol(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "protocol-driver")
	script := `#!/bin/sh
IFS= read -r request
printf '%s\n' '{"id":1,"result":{"connected":true,"stats":{"connect":1}}}'
IFS= read -r request
printf '%s\n' '{"id":2,"result":[{"tagId":"tag-1","name":"temperature","value":23.5,"quality":"good"}]}'
IFS= read -r request
printf '%s\n' '{"id":3,"result":{}}'
`
	if err := os.WriteFile(executable, []byte(script), 0750); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{
		Descriptor:  core.ExtensionDescriptor{Type: "protocol-driver", Capabilities: []string{"polling", "read"}},
		commandPath: executable,
	}
	driver := newDriverAdapter(manifest, "protocol-driver", core.DriverConfig{GroupID: "conn-1"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := driver.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	values, err := driver.ReadTags(ctx, []core.Tag{{ID: "tag-1", Name: "temperature"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 1 || values[0].Value != float64(23.5) || values[0].Quality != core.QualityGood {
		t.Fatalf("unexpected values: %#v", values)
	}
	if err := driver.Disconnect(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerWatchHotLoadsPlugin(t *testing.T) {
	directory := t.TempDir()
	drivers := core.NewDriverRegistry()
	manager := NewManager(directory, drivers, core.NewNorthRegistry())
	if _, err := manager.Reload(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changesCh := make(chan ChangeSet, 1)
	errorsCh := make(chan error, 1)
	if err := manager.Watch(ctx, func(changes ChangeSet) error {
		changesCh <- changes
		return nil
	}, func(err error) {
		errorsCh <- err
	}); err != nil {
		t.Fatal(err)
	}

	executable := filepath.Join(directory, "watched-driver")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\nexit 0\n"), 0750); err != nil {
		t.Fatal(err)
	}
	manifest := `{
  "apiVersion":"jetlinks-edge-plugin/v1",
  "kind":"driver",
  "command":"./watched-driver",
  "descriptor":{"type":"watched-driver","name":"Watched Driver","version":"1.0.0"}
}`
	if err := os.WriteFile(filepath.Join(directory, "watched.json"), []byte(manifest), 0640); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-errorsCh:
		t.Fatalf("watch reload failed: %v", err)
	case changes := <-changesCh:
		if len(changes.DriverTypes) != 1 || changes.DriverTypes[0] != "watched-driver" {
			t.Fatalf("unexpected watch changes: %#v", changes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for plugin hot load")
	}
	if _, exists := drivers.Descriptor("watched-driver"); !exists {
		t.Fatal("watched plugin was not registered")
	}
}

func TestProcessHostCall(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "host-call-plugin")
	script := `#!/bin/sh
printf '%s\n' '{"id":9001,"hostCall":true,"method":"ping","payload":{"value":"hello"}}'
IFS= read -r response
case "$response" in
  *'"hostCall":true'*'"pong":"ok"'*) exit 0 ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(executable, []byte(script), 0750); err != nil {
		t.Fatal(err)
	}
	called := make(chan struct{}, 1)
	client, err := startProcess(context.Background(), Manifest{
		Descriptor:  core.ExtensionDescriptor{Type: "host-call-plugin"},
		commandPath: executable,
	}, func(_ context.Context, method string, payload json.RawMessage) (interface{}, error) {
		if method != "ping" || string(payload) != `{"value":"hello"}` {
			return nil, fmt.Errorf("unexpected host call: %s %s", method, payload)
		}
		called <- struct{}{}
		return map[string]string{"pong": "ok"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer client.close()
	select {
	case <-called:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for host call")
	}
	select {
	case <-client.done:
		client.mu.Lock()
		err = client.err
		client.mu.Unlock()
		if err != nil {
			t.Fatalf("plugin rejected host response: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for plugin exit")
	}
}

func TestExternalNorthRestartsAfterProcessExit(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "restart-north")
	script := `#!/bin/sh
IFS= read -r start
printf '%s\n' '{"id":1,"result":{"connected":true}}'
IFS= read -r message
printf '%s\n' '{"id":2,"result":{}}'
exit 0
`
	if err := os.WriteFile(executable, []byte(script), 0750); err != nil {
		t.Fatal(err)
	}
	adapter, err := newNorthAdapter(context.Background(), Manifest{
		Descriptor:  core.ExtensionDescriptor{Type: "restart-north"},
		commandPath: executable,
	}, "north-1", core.NorthAppConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// 模拟插件处理消息后会主动退出，Close 可能返回已停止错误。
		_ = adapter.Close()
	})
	if err := adapter.OnMessage(context.Background(), core.NorthMessage{GroupID: "group-1"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		adapter.mu.RLock()
		running := adapter.client != nil && adapter.client.running()
		adapter.mu.RUnlock()
		if !running {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := adapter.OnMessage(context.Background(), core.NorthMessage{GroupID: "group-2"}); err != nil {
		t.Fatalf("message after plugin exit was not recovered: %v", err)
	}
}
