package externalplugin

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jhzhang09/jetlinks-edge/internal/core"
)

type driverAdapter struct {
	manifest Manifest
	name     string
	config   core.DriverConfig

	mu     sync.RWMutex
	client *processClient
	status core.DriverStatus
}

func newDriverAdapter(manifest Manifest, name string, config core.DriverConfig) *driverAdapter {
	return &driverAdapter{manifest: manifest, name: name, config: config, status: core.DriverStatus{Stats: map[string]int64{}}}
}

func (d *driverAdapter) Name() string { return d.name }

func (d *driverAdapter) Connect(ctx context.Context) error {
	d.mu.Lock()
	if d.client != nil {
		d.client.close()
		d.client = nil
	}
	client, err := startProcess(ctx, d.manifest, nil)
	if err == nil {
		var result core.DriverStatus
		err = client.call(ctx, "connect", map[string]interface{}{
			"instanceId": d.config.GroupID,
			"config":     d.config.Config,
		}, &result)
		if err == nil {
			d.client = client
			result.Connected = true
			result.LastTime = time.Now()
			d.status = result
		}
	}
	if err != nil {
		if client != nil {
			client.close()
		}
		d.status.Connected = false
		d.status.LastError = err.Error()
		d.status.LastTime = time.Now()
	}
	d.mu.Unlock()
	return err
}

func (d *driverAdapter) Disconnect() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := d.client.call(ctx, "disconnect", nil, nil)
	d.client.close()
	d.client = nil
	d.status.Connected = false
	d.status.LastTime = time.Now()
	return err
}

func (d *driverAdapter) Status() core.DriverStatus {
	d.mu.RLock()
	defer d.mu.RUnlock()
	status := d.status
	if d.client != nil && !d.client.running() {
		status.Connected = false
		status.LastError = "plugin process stopped"
	}
	return status
}

func (d *driverAdapter) ReadTags(ctx context.Context, tags []core.Tag) ([]core.TagValue, error) {
	client, err := d.runningClient(ctx)
	if err != nil {
		return nil, err
	}
	var values []core.TagValue
	if err := client.call(ctx, "readTags", map[string]interface{}{"tags": tags}, &values); err != nil {
		d.recordError(err)
		return nil, err
	}
	return values, nil
}

func (d *driverAdapter) WriteTag(ctx context.Context, tag core.Tag, value interface{}) error {
	client, err := d.runningClient(ctx)
	if err != nil {
		return err
	}
	err = client.call(ctx, "writeTag", map[string]interface{}{"tag": tag, "value": value}, nil)
	if err != nil {
		d.recordError(err)
	}
	return err
}

func (d *driverAdapter) InvokeFunction(ctx context.Context, functionID string, inputs interface{}) (interface{}, error) {
	client, err := d.runningClient(ctx)
	if err != nil {
		return nil, err
	}
	var output interface{}
	if err := client.call(ctx, "invokeFunction", map[string]interface{}{"functionId": functionID, "inputs": inputs}, &output); err != nil {
		d.recordError(err)
		return nil, err
	}
	return output, nil
}

func (d *driverAdapter) Browse(ctx context.Context, nodeID string) ([]core.NodeItem, error) {
	client, err := d.runningClient(ctx)
	if err != nil {
		return nil, err
	}
	var nodes []core.NodeItem
	if err := client.call(ctx, "browse", map[string]interface{}{"nodeId": nodeID}, &nodes); err != nil {
		d.recordError(err)
		return nil, err
	}
	return nodes, nil
}

func (d *driverAdapter) runningClient(ctx context.Context) (*processClient, error) {
	d.mu.RLock()
	client := d.client
	d.mu.RUnlock()
	if client != nil && client.running() {
		return client, nil
	}
	if err := d.Connect(ctx); err != nil {
		return nil, fmt.Errorf("reconnect external driver %s: %w", d.name, err)
	}
	d.mu.RLock()
	client = d.client
	d.mu.RUnlock()
	if client == nil {
		return nil, fmt.Errorf("external driver %s is not connected", d.name)
	}
	return client, nil
}

func (d *driverAdapter) recordError(err error) {
	d.mu.Lock()
	d.status.LastError = err.Error()
	d.status.LastTime = time.Now()
	d.mu.Unlock()
}

type northAdapter struct {
	manifest  Manifest
	appID     string
	config    core.NorthAppConfig
	ctx       context.Context
	client    *processClient
	cancel    context.CancelFunc
	mu        sync.RWMutex
	restartMu sync.Mutex
	state     core.NorthState
	closed    bool
}

func newNorthAdapter(ctx context.Context, manifest Manifest, appID string, config core.NorthAppConfig) (*northAdapter, error) {
	appCtx, cancel := context.WithCancel(ctx)
	adapter := &northAdapter{
		manifest: manifest,
		appID:    appID,
		config:   config,
		ctx:      appCtx,
		cancel:   cancel,
		state:    core.NorthState{Stats: map[string]int64{}},
	}
	if err := adapter.start(); err != nil {
		cancel()
		return nil, err
	}
	return adapter, nil
}

func (a *northAdapter) handleHostCall(callCtx context.Context, method string, payload json.RawMessage) (interface{}, error) {
	switch method {
	case "executeCommand":
		if a.config.CommandExecutor == nil {
			return nil, fmt.Errorf("command executor unavailable")
		}
		var command core.NorthCommand
		if err := json.Unmarshal(payload, &command); err != nil {
			return nil, fmt.Errorf("decode command: %w", err)
		}
		return a.config.CommandExecutor(callCtx, command)
	case "groupStatus":
		if a.config.GroupStatusProvider == nil {
			return nil, fmt.Errorf("group status provider unavailable")
		}
		var request struct {
			GroupID string `json:"groupId"`
		}
		if err := json.Unmarshal(payload, &request); err != nil {
			return nil, fmt.Errorf("decode group status request: %w", err)
		}
		status, exists := a.config.GroupStatusProvider(request.GroupID)
		return map[string]interface{}{"exists": exists, "status": status}, nil
	default:
		return nil, fmt.Errorf("unsupported host call: %s", method)
	}
}

func (a *northAdapter) start() error {
	client, err := startProcess(a.ctx, a.manifest, a.handleHostCall)
	if err != nil {
		return err
	}
	var state core.NorthState
	if err := client.call(a.ctx, "start", map[string]interface{}{"instanceId": a.appID, "config": a.config.Config}, &state); err != nil {
		client.close()
		return err
	}
	a.mu.Lock()
	old := a.client
	a.client = client
	a.state = state
	a.mu.Unlock()
	if old != nil {
		old.close()
	}
	return nil
}

func (a *northAdapter) runningClient() (*processClient, error) {
	a.mu.RLock()
	client := a.client
	closed := a.closed
	a.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("external north plugin is stopped")
	}
	if client != nil && client.running() {
		return client, nil
	}
	a.restartMu.Lock()
	defer a.restartMu.Unlock()
	a.mu.RLock()
	client = a.client
	closed = a.closed
	a.mu.RUnlock()
	if closed {
		return nil, fmt.Errorf("external north plugin is stopped")
	}
	if client != nil && client.running() {
		return client, nil
	}
	if err := a.start(); err != nil {
		return nil, fmt.Errorf("restart external north plugin: %w", err)
	}
	a.mu.RLock()
	client = a.client
	a.mu.RUnlock()
	return client, nil
}

func (a *northAdapter) OnMessage(ctx context.Context, message core.NorthMessage) error {
	client, err := a.runningClient()
	if err != nil {
		return err
	}
	err = client.call(ctx, "onMessage", message, nil)
	if err != nil {
		a.mu.Lock()
		a.state.LastError = err.Error()
		a.mu.Unlock()
	}
	return err
}

func (a *northAdapter) Close() error {
	a.restartMu.Lock()
	defer a.restartMu.Unlock()
	a.mu.Lock()
	a.closed = true
	if a.client == nil {
		a.mu.Unlock()
		a.cancel()
		return nil
	}
	client := a.client
	a.client = nil
	a.state.Connected = false
	a.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := client.call(ctx, "stop", nil, nil)
	client.close()
	a.cancel()
	return err
}

func (a *northAdapter) State() *core.NorthState {
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := a.state
	if a.client != nil && !a.client.running() {
		state.Connected = false
		state.LastError = "plugin process stopped"
	}
	state.Stats = cloneStats(state.Stats)
	return &state
}

func cloneStats(source map[string]int64) map[string]int64 {
	if source == nil {
		return nil
	}
	out := make(map[string]int64, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
