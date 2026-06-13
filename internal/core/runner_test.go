package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestUpdateCachedValueDetectsChangesBeforeOverwrite(t *testing.T) {
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)

	first := TagValue{TagID: "tag-1", Value: 1}
	if !runner.updateCachedValue("group-1", first) {
		t.Fatal("first value must be treated as changed")
	}
	if runner.updateCachedValue("group-1", first) {
		t.Fatal("same value must not be treated as changed")
	}
	if !runner.updateCachedValue("group-1", TagValue{TagID: "tag-1", Value: 2}) {
		t.Fatal("new value must be treated as changed")
	}
}

type lifecycleNorth struct {
	registered   int
	deregistered int
	closed       int
}

func (n *lifecycleNorth) OnMessage(context.Context, NorthMessage) error {
	return nil
}

func (n *lifecycleNorth) OnCommand(context.Context, NorthCommand) (NorthCommandReply, error) {
	return NorthCommandReply{}, nil
}

func (n *lifecycleNorth) RegisterGroup(*Group) bool {
	n.registered++
	return true
}

func (n *lifecycleNorth) DeregisterGroup(*Group) {
	n.deregistered++
}

func (n *lifecycleNorth) Close() error {
	n.closed++
	return nil
}

func TestStartNorthAppReplacesLifecycleAndRebindsGroups(t *testing.T) {
	registry := NewNorthRegistry()
	var instances []*lifecycleNorth
	registry.Register("test", func(context.Context, string, NorthAppConfig) (NorthHandler, error) {
		instance := &lifecycleNorth{}
		instances = append(instances, instance)
		return instance, nil
	})
	runner := NewRunner(NewDriverRegistry(), registry, nil)
	group := &Group{ID: "group-1", NorthAppID: "north-1"}
	runner.groups[group.ID] = &groupRuntime{group: group}

	app := &NorthApp{ID: "north-1", Type: "test", Enabled: true}
	if err := runner.startNorthApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if instances[0].registered != 1 {
		t.Fatalf("first instance registered = %d, want 1", instances[0].registered)
	}

	if err := runner.startNorthApp(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	if instances[0].deregistered != 1 || instances[0].closed != 1 {
		t.Fatalf("old instance lifecycle: deregistered=%d closed=%d, want 1/1",
			instances[0].deregistered, instances[0].closed)
	}
	if instances[1].registered != 1 {
		t.Fatalf("new instance registered = %d, want 1", instances[1].registered)
	}
}

type commandDriver struct {
	readCount  int
	writeCount int
	writeValue interface{}
	readValue  TagValue
	status     DriverStatus
}

func (d *commandDriver) Name() string { return "test" }
func (d *commandDriver) Connect(context.Context) error {
	return nil
}
func (d *commandDriver) ReadTags(_ context.Context, tags []Tag) ([]TagValue, error) {
	d.readCount++
	value := d.readValue
	value.TagID = tags[0].ID
	value.Name = tags[0].Name
	if value.Quality == "" {
		value.Quality = QualityGood
	}
	return []TagValue{value}, nil
}
func (d *commandDriver) WriteTag(_ context.Context, _ Tag, value interface{}) error {
	d.writeCount++
	d.writeValue = value
	return nil
}
func (d *commandDriver) Disconnect() error { return nil }
func (d *commandDriver) Status() DriverStatus {
	return d.status
}

type collectDriver struct {
	values []TagValue
	err    error
}

func (d *collectDriver) Name() string { return "test-collect" }
func (d *collectDriver) Connect(context.Context) error {
	return nil
}
func (d *collectDriver) ReadTags(context.Context, []Tag) ([]TagValue, error) {
	values := make([]TagValue, len(d.values))
	copy(values, d.values)
	return values, d.err
}
func (d *collectDriver) WriteTag(context.Context, Tag, interface{}) error {
	return nil
}
func (d *collectDriver) Disconnect() error { return nil }
func (d *collectDriver) Status() DriverStatus {
	return DriverStatus{}
}

type recordingNorth struct {
	mu       sync.Mutex
	messages []NorthMessage
}

func (n *recordingNorth) OnMessage(_ context.Context, msg NorthMessage) error {
	n.mu.Lock()
	n.messages = append(n.messages, msg)
	n.mu.Unlock()
	return nil
}

func (n *recordingNorth) OnCommand(context.Context, NorthCommand) (NorthCommandReply, error) {
	return NorthCommandReply{}, nil
}

// snapshot 线程安全地返回当前所有收到的消息副本。
// 测试主协程用它来读，避免与 OnMessage 的 append 产生 data race。
func (n *recordingNorth) snapshot() []NorthMessage {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := make([]NorthMessage, len(n.messages))
	copy(out, n.messages)
	return out
}

func TestExecuteNorthCommandReadsAndWritesTags(t *testing.T) {
	driver := &commandDriver{readValue: TagValue{Value: 42}}
	group := &Group{
		ID:     "group-1",
		Device: DeviceConfig{ProductID: "product-1", DeviceID: "device-1"},
		Tags: []Tag{
			{ID: "read-tag", Name: "temperature", Access: AccessRO},
			{ID: "write-tag", Name: "setpoint", Access: AccessRW},
		},
	}
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	runner.groups[group.ID] = &groupRuntime{group: group, connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver}}

	readReply, err := runner.executeNorthCommand(context.Background(), NorthCommand{
		ID:       "read-1",
		DeviceID: "device-1",
		Type:     "read-property",
		Payload:  map[string]interface{}{"properties": []interface{}{"temperature"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if readReply.Code != 0 || readReply.Payload["temperature"] != 42 {
		t.Fatalf("unexpected read reply: %+v", readReply)
	}

	writeReply, err := runner.executeNorthCommand(context.Background(), NorthCommand{
		ID:       "write-1",
		DeviceID: "device-1",
		Type:     "write-property",
		Payload:  map[string]interface{}{"properties": map[string]interface{}{"setpoint": 55}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if writeReply.Code != 0 || driver.writeCount != 1 || driver.writeValue != 55 {
		t.Fatalf("unexpected write result: reply=%+v count=%d value=%v", writeReply, driver.writeCount, driver.writeValue)
	}
}

func TestExecuteNorthCommandRejectsBadQualityRead(t *testing.T) {
	driver := &commandDriver{readValue: TagValue{Quality: QualityBad, Error: "device offline"}}
	group := &Group{
		ID:     "group-1",
		Device: DeviceConfig{ProductID: "product-1", DeviceID: "device-1"},
		Tags:   []Tag{{ID: "read-tag", Name: "temperature", Access: AccessRO}},
	}
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	runner.groups[group.ID] = &groupRuntime{group: group, connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver}}

	reply, err := runner.executeNorthCommand(context.Background(), NorthCommand{
		ID:       "read-1",
		DeviceID: "device-1",
		Type:     "read-property",
		Payload:  map[string]interface{}{"properties": []interface{}{"temperature"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Code != 500 || reply.Message != "device offline" {
		t.Fatalf("unexpected read reply: %+v", reply)
	}
}

func TestCollectOnceSkipsNorthReportWhenNoGoodValues(t *testing.T) {
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	north := &recordingNorth{}
	group := &Group{
		ID:           "group-1",
		ConnectionID: "conn-1",
		Device:       DeviceConfig{ProductID: "product-1", DeviceID: "device-1"},
		Tags:         []Tag{{ID: "temperature", Name: "temperature"}},
	}
	driver := &collectDriver{
		values: []TagValue{{Quality: QualityBad, Error: "device offline"}},
	}

	topic := fmt.Sprintf("south/connection/%s/group/%s/values", "conn-1", group.ID)
	ch, unsub := runner.eventBus.Subscribe(topic, 10)
	defer unsub()
	go func() {
		for ev := range ch {
			if msg, ok := ev.Payload.(NorthMessage); ok {
				_ = north.OnMessage(context.Background(), msg)
			}
		}
	}()

	runner.collectOnce(context.Background(), &groupRuntime{
		group:  group,
		connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver},
		norths: []NorthHandler{north},
	})

	if len(north.snapshot()) != 0 {
		t.Fatalf("north messages = %d, want 0", len(north.snapshot()))
	}
	values := runner.LastValues(group.ID)
	if values["temperature"].Quality != QualityBad {
		t.Fatalf("cached quality = %q, want %q", values["temperature"].Quality, QualityBad)
	}
}

func TestCollectOnceReportsOnlyGoodQualityValues(t *testing.T) {
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	north := &recordingNorth{}
	group := &Group{
		ID:           "group-1",
		ConnectionID: "conn-1",
		Device:       DeviceConfig{ProductID: "product-1", DeviceID: "device-1"},
		Tags: []Tag{
			{ID: "temperature", Name: "temperature"},
			{ID: "pressure", Name: "pressure"},
		},
	}
	driver := &collectDriver{
		values: []TagValue{
			{Value: 25, Quality: QualityGood},
			{Quality: QualityBad, Error: "device offline"},
		},
	}

	topic := fmt.Sprintf("south/connection/%s/group/%s/values", "conn-1", group.ID)
	ch, unsub := runner.eventBus.Subscribe(topic, 10)
	defer unsub()
	go func() {
		for ev := range ch {
			if msg, ok := ev.Payload.(NorthMessage); ok {
				_ = north.OnMessage(context.Background(), msg)
			}
		}
	}()

	runner.collectOnce(context.Background(), &groupRuntime{
		group:  group,
		connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver},
		norths: []NorthHandler{north},
	})

	// 等待一小会儿确保协程消费完毕
	time.Sleep(10 * time.Millisecond)

	msgs := north.snapshot()
	if len(msgs) != 1 {
		t.Fatalf("north messages = %d, want 1", len(msgs))
	}
	properties, ok := msgs[0].Payload["properties"].(map[string]interface{})
	if !ok {
		t.Fatalf("properties payload type = %T, want map[string]interface{}", msgs[0].Payload["properties"])
	}
	if len(properties) != 1 || properties["temperature"] != 25 {
		t.Fatalf("properties = %#v, want only temperature=25", properties)
	}
	if _, ok := properties["pressure"]; ok {
		t.Fatalf("bad quality value must not be reported: %#v", properties)
	}

	changes, ok := msgs[0].Payload["changes"].(map[string]interface{})
	if !ok {
		t.Fatalf("changes payload type = %T, want map[string]interface{}", msgs[0].Payload["changes"])
	}
	if len(changes) != 1 || changes["temperature"] != 25 {
		t.Fatalf("changes = %#v, want only temperature=25", changes)
	}
}

func TestReadWriteTagEnforcesAccess(t *testing.T) {
	driver := &commandDriver{}
	group := &Group{
		ID: "group-1",
		Tags: []Tag{
			{ID: "write-only", Access: AccessWO},
			{ID: "read-only", Access: AccessRO},
		},
	}
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	runner.groups[group.ID] = &groupRuntime{group: group, connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver}}

	if _, err := runner.ReadTag(context.Background(), group.ID, "write-only"); !errors.Is(err, ErrTagWriteOnly) {
		t.Fatalf("ReadTag error = %v, want ErrTagWriteOnly", err)
	}
	if err := runner.WriteTag(context.Background(), group.ID, "read-only", 1); !errors.Is(err, ErrTagReadOnly) {
		t.Fatalf("WriteTag error = %v, want ErrTagReadOnly", err)
	}
}

func TestGroupStatusReturnsDriverStatus(t *testing.T) {
	driver := &commandDriver{status: DriverStatus{Connected: true, LastError: "last-error"}}
	runner := NewRunner(NewDriverRegistry(), NewNorthRegistry(), nil)
	runner.groups["group-1"] = &groupRuntime{group: &Group{ID: "group-1"}, connRt: &connectionRuntime{conn: &Connection{ID: "conn-1"}, driver: driver}}

	status, ok := runner.groupStatus("group-1")
	if !ok {
		t.Fatal("groupStatus ok = false, want true")
	}
	if !status.Connected || status.LastError != "last-error" {
		t.Fatalf("status = %+v, want connected with last-error", status)
	}
}

func TestNorthRegistryPassesInstanceIDToFactory(t *testing.T) {
	registry := NewNorthRegistry()
	var received string
	registry.Register("test-type", func(_ context.Context, appID string, _ NorthAppConfig) (NorthHandler, error) {
		received = appID
		return &lifecycleNorth{}, nil
	})
	if _, err := registry.Create(context.Background(), "test-type", NorthAppConfig{AppID: "north-1"}); err != nil {
		t.Fatal(err)
	}
	if received != "north-1" {
		t.Fatalf("factory appID = %q, want %q", received, "north-1")
	}
}

func TestEventBusMatchTopic(t *testing.T) {
	cases := []struct {
		pattern string
		topic   string
		want    bool
	}{
		{"a/b/c", "a/b/c", true},
		{"a/b/c", "a/b/d", false},
		{"a/+/c", "a/b/c", true},
		{"a/+/c", "a/foo/c", true},
		{"a/+/c", "a/b/d", false},
		{"a/#", "a/b/c", true},
		{"a/#", "a/b", true},
		{"#", "a/b/c", true},
		{"a/b/+/d", "a/b/c/d", true},
		{"a/b/+/d", "a/b/c/e", false},
	}
	for _, tc := range cases {
		got := matchTopic(tc.pattern, tc.topic)
		if got != tc.want {
			t.Errorf("matchTopic(%q, %q) = %v, want %v", tc.pattern, tc.topic, got, tc.want)
		}
	}
}

func TestEventBusPublishSubscribe(t *testing.T) {
	eb := NewEventBus()
	ch1, unsub1 := eb.Subscribe("south/+/group-1/values", 10)
	defer unsub1()

	ch2, unsub2 := eb.Subscribe("south/#", 10)
	// 不用 defer unsub2，后面测试取消订阅

	ev := Event{
		Topic: "south/conn-1/group-1/values",
		Type:  "values",
		Payload: "payload-data",
	}
	eb.Publish(ev.Topic, ev)

	select {
	case e := <-ch1:
		if e.Payload != "payload-data" {
			t.Errorf("ch1 payload = %v, want payload-data", e.Payload)
		}
	default:
		t.Error("ch1 did not receive event")
	}

	select {
	case e := <-ch2:
		if e.Payload != "payload-data" {
			t.Errorf("ch2 payload = %v, want payload-data", e.Payload)
		}
	default:
		t.Error("ch2 did not receive event")
	}

	// 测试取消订阅
	unsub2()
	eb.Publish(ev.Topic, ev)

	select {
	case <-ch2:
		t.Error("ch2 received event after unsubscribe")
	default:
		// 期望没有收到
	}
}

type mockNorthHandler struct {
	mu       sync.Mutex
	messages []NorthMessage
}

func (m *mockNorthHandler) OnMessage(ctx context.Context, msg NorthMessage) error {
	m.mu.Lock()
	m.messages = append(m.messages, msg)
	m.mu.Unlock()
	return nil
}

func (m *mockNorthHandler) OnCommand(ctx context.Context, cmd NorthCommand) (NorthCommandReply, error) {
	return NorthCommandReply{}, nil
}

// snapshot 线程安全地返回当前所有收到的消息副本。
func (m *mockNorthHandler) snapshot() []NorthMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]NorthMessage, len(m.messages))
	copy(out, m.messages)
	return out
}

func TestRunnerIntegratesEventBus(t *testing.T) {
	drivers := NewDriverRegistry()
	drivers.Register("test-driver", func(ctx context.Context, name string, cfg DriverConfig) (SouthDriver, error) {
		return &commandDriver{
			readValue: TagValue{Value: 88},
		}, nil
	})

	north := NewNorthRegistry()
	northHandler := &mockNorthHandler{}
	north.Register("test-north", func(ctx context.Context, appID string, cfg NorthAppConfig) (NorthHandler, error) {
		return northHandler, nil
	})

	store := &memStore{
		groups: map[string]*Group{
			"group-1": {
				ID:           "group-1",
				Enabled:      true,
				ConnectionID: "conn-1",
				IntervalMs:   10,
				NorthAppID:   "north-1",
			},
		},
		tags: map[string][]*Tag{
			"group-1": {
				{ID: "tag-1", Name: "tag-1", Access: AccessRO},
			},
		},
		connections: map[string]*Connection{
			"conn-1": {
				ID:      "conn-1",
				Driver:  "test-driver",
				Enabled: true,
			},
		},
		northApps: map[string]*NorthApp{
			"north-1": {
				ID:      "north-1",
				Type:    "test-north",
				Enabled: true,
			},
		},
	}

	logger, _ := zap.NewDevelopment()
	zap.ReplaceGlobals(logger)

	runner := NewRunner(drivers, north, store)
	
	// 通过总线额外订阅，测试多订阅者路由能力
	busCh, busUnsub := runner.EventBus().Subscribe("south/connection/conn-1/group/group-1/values", 10)
	defer busUnsub()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := runner.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer runner.Stop()

	// 等待采集一轮
	select {
	case ev := <-busCh:
		if msg, ok := ev.Payload.(NorthMessage); ok {
			props := msg.Payload["properties"].(map[string]interface{})
			if props["tag-1"] != 88 {
				t.Fatalf("bus received tag-1 = %v, want 88", props["tag-1"])
			}
		} else {
			t.Fatalf("bus received unexpected payload: %T", ev.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for EventBus publish")
	}

	// 验证内置桥接器已经自动把数据推给了 northHandler
	// 桥接的消费可能稍微有一点时延，这里等待一下
	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if len(northHandler.snapshot()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	msgs := northHandler.snapshot()
	if len(msgs) == 0 {
		t.Fatal("northHandler did not receive bridged OnMessage call")
	}

	props := msgs[0].Payload["properties"].(map[string]interface{})
	if props["tag-1"] != 88 {
		t.Fatalf("northHandler received tag-1 = %v, want 88", props["tag-1"])
	}
}

type memStore struct {
	groups      map[string]*Group
	tags        map[string][]*Tag
	connections map[string]*Connection
	northApps   map[string]*NorthApp
}

func (m *memStore) ListEnabledGroups(ctx context.Context) ([]*Group, error) {
	var out []*Group
	for _, g := range m.groups {
		if g.Enabled {
			out = append(out, g)
		}
	}
	return out, nil
}
func (m *memStore) GetGroup(ctx context.Context, id string) (*Group, error) {
	return m.groups[id], nil
}
func (m *memStore) SaveGroup(ctx context.Context, g *Group) error {
	m.groups[g.ID] = g
	return nil
}
func (m *memStore) DeleteGroup(ctx context.Context, id string) error {
	delete(m.groups, id)
	return nil
}
func (m *memStore) SaveTag(ctx context.Context, t *Tag) error {
	return nil
}
func (m *memStore) DeleteTag(ctx context.Context, id string) error {
	return nil
}
func (m *memStore) ListTagsByGroup(ctx context.Context, groupID string) ([]*Tag, error) {
	return m.tags[groupID], nil
}
func (m *memStore) GetTag(ctx context.Context, id string) (*Tag, error) {
	return nil, nil
}
func (m *memStore) ListNorthApps(ctx context.Context) ([]*NorthApp, error) {
	return nil, nil
}
func (m *memStore) ListEnabledNorthApps(ctx context.Context) ([]*NorthApp, error) {
	var out []*NorthApp
	for _, n := range m.northApps {
		if n.Enabled {
			out = append(out, n)
		}
	}
	return out, nil
}
func (m *memStore) GetNorthApp(ctx context.Context, id string) (*NorthApp, error) {
	return m.northApps[id], nil
}
func (m *memStore) SaveNorthApp(ctx context.Context, n *NorthApp) error {
	m.northApps[n.ID] = n
	return nil
}
func (m *memStore) DeleteNorthApp(ctx context.Context, id string) error {
	delete(m.northApps, id)
	return nil
}
func (m *memStore) ListEnabledConnections(ctx context.Context) ([]*Connection, error) {
	var out []*Connection
	for _, c := range m.connections {
		if c.Enabled {
			out = append(out, c)
		}
	}
	return out, nil
}
func (m *memStore) GetConnection(ctx context.Context, id string) (*Connection, error) {
	return m.connections[id], nil
}
func (m *memStore) SaveConnection(ctx context.Context, conn *Connection) error {
	m.connections[conn.ID] = conn
	return nil
}
func (m *memStore) DeleteConnection(ctx context.Context, id string) error {
	delete(m.connections, id)
	return nil
}

