package core

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	northDeliveryQueueSize = 1024
	northDeliveryAttempts  = 5
)

// queuedNorth 为每个北向应用提供独立、有界、可重试的投递队列。
// 瞬时网络故障不会阻塞采集组的事件桥接协程，持续故障则通过状态计数显式暴露。
type queuedNorth struct {
	target NorthMessageHandler
	ctx    context.Context
	cancel context.CancelFunc
	queue  chan NorthMessage
	wg     sync.WaitGroup

	enqueued  atomic.Int64
	delivered atomic.Int64
	failed    atomic.Int64
	dropped   atomic.Int64
}

func newQueuedNorth(parent context.Context, target NorthMessageHandler) *queuedNorth {
	ctx, cancel := context.WithCancel(parent)
	queued := &queuedNorth{
		target: target,
		ctx:    ctx,
		cancel: cancel,
		queue:  make(chan NorthMessage, northDeliveryQueueSize),
	}
	queued.wg.Add(1)
	go queued.run()
	return queued
}

func (q *queuedNorth) OnMessage(ctx context.Context, message NorthMessage) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.ctx.Done():
		return q.ctx.Err()
	case q.queue <- message:
		q.enqueued.Add(1)
		return nil
	default:
		q.dropped.Add(1)
		return fmt.Errorf("north delivery queue is full")
	}
}

func (q *queuedNorth) run() {
	defer q.wg.Done()
	for {
		select {
		case <-q.ctx.Done():
			return
		case message := <-q.queue:
			q.deliver(message)
		}
	}
}

func (q *queuedNorth) deliver(message NorthMessage) {
	delay := 100 * time.Millisecond
	for attempt := 1; attempt <= northDeliveryAttempts; attempt++ {
		if err := q.target.OnMessage(q.ctx, message); err == nil {
			q.delivered.Add(1)
			return
		}
		if attempt == northDeliveryAttempts {
			q.failed.Add(1)
			return
		}
		timer := time.NewTimer(delay)
		select {
		case <-q.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		if delay < 2*time.Second {
			delay *= 2
		}
	}
}

func (q *queuedNorth) Close() error {
	q.cancel()
	q.wg.Wait()
	if lifecycle, ok := q.target.(NorthLifecycle); ok {
		return lifecycle.Close()
	}
	return nil
}

func (q *queuedNorth) State() *NorthState {
	state := &NorthState{Connected: true, Stats: map[string]int64{}}
	if reporter, ok := q.target.(NorthStateReporter); ok {
		if reported := reporter.State(); reported != nil {
			state.Connected = reported.Connected
			state.LastError = reported.LastError
			for key, value := range reported.Stats {
				state.Stats[key] = value
			}
		}
	}
	state.Stats["deliveryEnqueued"] = q.enqueued.Load()
	state.Stats["deliverySucceeded"] = q.delivered.Load()
	state.Stats["deliveryFailed"] = q.failed.Load()
	state.Stats["deliveryDropped"] = q.dropped.Load()
	state.Stats["deliveryQueued"] = int64(len(q.queue))
	return state
}

func (q *queuedNorth) RegisterGroup(group *Group) bool {
	if register, ok := q.target.(northRegister); ok {
		return register.RegisterGroup(group)
	}
	return true
}

func (q *queuedNorth) DeregisterGroup(group *Group) {
	if register, ok := q.target.(northRegister); ok {
		register.DeregisterGroup(group)
	}
}
