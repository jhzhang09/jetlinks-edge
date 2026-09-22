package externalplugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"go.uber.org/zap"
)

type rpcRequest struct {
	ID       uint64      `json:"id"`
	HostCall bool        `json:"hostCall,omitempty"`
	Method   string      `json:"method"`
	Payload  interface{} `json:"payload,omitempty"`
}

type rpcResponse struct {
	ID       uint64          `json:"id"`
	HostCall bool            `json:"hostCall,omitempty"`
	Method   string          `json:"method,omitempty"`
	Payload  json.RawMessage `json:"payload,omitempty"`
	Result   json.RawMessage `json:"result,omitempty"`
	Error    string          `json:"error,omitempty"`
}

type hostCallHandler func(context.Context, string, json.RawMessage) (interface{}, error)

type processClient struct {
	cancel context.CancelFunc
	cmd    *exec.Cmd
	stdin  io.WriteCloser

	writeMu  sync.Mutex
	mu       sync.Mutex
	pending  map[uint64]chan rpcResponse
	done     chan struct{}
	nextID   atomic.Uint64
	once     sync.Once
	err      error
	ctx      context.Context
	hostCall hostCallHandler
}

func startProcess(ctx context.Context, manifest Manifest, hostCall hostCallHandler) (*processClient, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, manifest.commandPath, manifest.Args...)
	cmd.Dir = filepath.Dir(manifest.commandPath)
	cmd.Env = pluginEnvironment()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	cmd.Stderr = pluginLogWriter{pluginType: manifest.Descriptor.Type}
	if err := cmd.Start(); err != nil {
		cancel()
		return nil, err
	}
	client := &processClient{
		cancel:   cancel,
		cmd:      cmd,
		stdin:    stdin,
		pending:  make(map[uint64]chan rpcResponse),
		done:     make(chan struct{}),
		ctx:      processCtx,
		hostCall: hostCall,
	}
	go client.readLoop(stdout)
	go func() {
		err := cmd.Wait()
		client.finish(err)
	}()
	return client, nil
}

func pluginEnvironment() []string {
	keys := []string{"PATH", "LANG", "LC_ALL", "TZ", "SYSTEMROOT", "WINDIR"}
	environment := make([]string, 0, len(keys)+1)
	for _, key := range keys {
		if value, exists := os.LookupEnv(key); exists {
			environment = append(environment, key+"="+value)
		}
	}
	return append(environment, "JETLINKS_EDGE_PLUGIN_PROTOCOL="+ProtocolVersion)
}

func (c *processClient) call(ctx context.Context, method string, payload, result interface{}) error {
	id := c.nextID.Add(1)
	responseCh := make(chan rpcResponse, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		err := c.err
		c.mu.Unlock()
		if err == nil {
			err = errors.New("plugin process stopped")
		}
		return err
	default:
	}
	c.pending[id] = responseCh
	c.mu.Unlock()

	c.writeMu.Lock()
	err := json.NewEncoder(c.stdin).Encode(rpcRequest{ID: id, Method: method, Payload: payload})
	c.writeMu.Unlock()
	if err != nil {
		c.removePending(id)
		return fmt.Errorf("send plugin request %s: %w", method, err)
	}

	select {
	case <-ctx.Done():
		c.removePending(id)
		return ctx.Err()
	case <-c.done:
		select {
		case response := <-responseCh:
			return decodeRPCResponse(method, response, result)
		default:
			c.removePending(id)
			if c.err != nil {
				return c.err
			}
			return errors.New("plugin process stopped")
		}
	case response := <-responseCh:
		return decodeRPCResponse(method, response, result)
	}
}

func decodeRPCResponse(method string, response rpcResponse, result interface{}) error {
	if response.Error != "" {
		return errors.New(response.Error)
	}
	if result == nil || len(response.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(response.Result, result); err != nil {
		return fmt.Errorf("decode plugin response %s: %w", method, err)
	}
	return nil
}

func (c *processClient) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var response rpcResponse
		if err := json.Unmarshal(scanner.Bytes(), &response); err != nil {
			c.finish(err)
			return
		}
		if response.HostCall {
			go c.handleHostCall(response)
			continue
		}
		c.mu.Lock()
		responseCh := c.pending[response.ID]
		delete(c.pending, response.ID)
		c.mu.Unlock()
		if responseCh != nil {
			responseCh <- response
		}
	}
	c.finish(scanner.Err())
}

func (c *processClient) handleHostCall(request rpcResponse) {
	response := rpcResponse{ID: request.ID, HostCall: true}
	if c.hostCall == nil {
		response.Error = "host calls are not supported by this plugin kind"
	} else {
		result, err := c.hostCall(c.ctx, request.Method, request.Payload)
		if err != nil {
			response.Error = err.Error()
		} else if result != nil {
			encoded, encodeErr := json.Marshal(result)
			if encodeErr != nil {
				response.Error = encodeErr.Error()
			} else {
				response.Result = encoded
			}
		}
	}
	c.writeMu.Lock()
	err := json.NewEncoder(c.stdin).Encode(response)
	c.writeMu.Unlock()
	if err != nil {
		c.finish(err)
	}
}

func (c *processClient) removePending(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *processClient) finish(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
		c.cancel()
	})
}

func (c *processClient) close() {
	c.finish(nil)
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

func (c *processClient) running() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

type pluginLogWriter struct{ pluginType string }

func (w pluginLogWriter) Write(data []byte) (int, error) {
	message := strings.TrimSpace(string(data))
	if message != "" {
		zap.L().Info("external plugin", zap.String("pluginType", w.pluginType), zap.String("message", message))
	}
	return len(data), nil
}
