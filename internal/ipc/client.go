package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Client provides a thread-safe RPC interface to the PC Remote background service
type Client struct {
	pipeName string
	conn     *PipeConn
	reader   *bufio.Reader
	mu       sync.Mutex
	reqSeq   uint64
	closed   bool
}

// NewClient creates a new client configured for the target pipe
func NewClient(pipeName string) *Client {
	if pipeName == "" {
		pipeName = PipeName
	}
	return &Client{
		pipeName: pipeName,
	}
}

// Connect attempts an immediate connection to the IPC server with a timeout
func (c *Client) Connect(timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ensureConnectedLocked(timeout)
}

// Close disconnects the client
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.reader = nil
		return err
	}
	return nil
}

func (c *Client) ensureConnectedLocked(timeout time.Duration) error {
	if c.closed {
		return errors.New("client closed")
	}
	if c.conn != nil {
		return nil
	}
	conn, err := Dial(c.pipeName, timeout)
	if err != nil {
		return err
	}
	c.conn = conn
	c.reader = bufio.NewReader(conn)
	return nil
}

// Call executes a JSON-RPC request and unmarshals the result into target
func (c *Client) Call(method string, params interface{}, target interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.ensureConnectedLocked(3 * time.Second); err != nil {
		return fmt.Errorf("service unreachable: %w", err)
	}

	c.reqSeq++
	req := Request{
		ID:     fmt.Sprintf("%d", c.reqSeq),
		Method: method,
	}
	if params != nil {
		raw, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to encode parameters: %w", err)
		}
		req.Params = raw
	}

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to encode request: %w", err)
	}
	reqBytes = append(reqBytes, '\n')

	if _, err := c.conn.Write(reqBytes); err != nil {
		// Connection failed; close and force reconnect next time
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
		return fmt.Errorf("IPC write error: %w", err)
	}

	line, isPrefix, err := c.reader.ReadLine()
	if err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
		return fmt.Errorf("IPC read error: %w", err)
	}
	if isPrefix {
		_ = c.conn.Close()
		c.conn = nil
		c.reader = nil
		return errors.New("IPC response exceeded buffer size")
	}

	var resp Response
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("invalid IPC response format: %w", err)
	}

	if resp.Error != "" {
		return errors.New(resp.Error)
	}

	if target != nil && len(resp.Result) > 0 {
		if err := json.Unmarshal(resp.Result, target); err != nil {
			return fmt.Errorf("failed to decode response result: %w", err)
		}
	}
	return nil
}

// GetStatus queries service operational metrics
func (c *Client) GetStatus() (*StatusResult, error) {
	var res StatusResult
	if err := c.Call(MethodGetStatus, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetPairingInfo queries the dynamic pairing URL and current 128-bit pairing token
func (c *Client) GetPairingInfo() (*PairingInfoResult, error) {
	var res PairingInfoResult
	if err := c.Call(MethodGetPairingInfo, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// RotatePairingToken requests the service to generate a fresh pairing token
func (c *Client) RotatePairingToken() (*PairingInfoResult, error) {
	var res PairingInfoResult
	if err := c.Call(MethodRotatePairingToken, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// GetPinStatus checks whether the Master PIN has been configured
func (c *Client) GetPinStatus() (*PinStatusResult, error) {
	var res PinStatusResult
	if err := c.Call(MethodGetPinStatus, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SetPin updates the Master PIN with authentication
func (c *Client) SetPin(oldPin, newPin string) (*SetPinResult, error) {
	var res SetPinResult
	params := SetPinParams{
		OldPin: oldPin,
		NewPin: newPin,
	}
	if err := c.Call(MethodSetPin, params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// MigrateLegacyKey safely transfers legacy configuration over local IPC for service DPAPI protection
func (c *Client) MigrateLegacyKey(params interface{}) (*MigrateKeyResult, error) {
	var res MigrateKeyResult
	var p MigrateKeyParams
	switch v := params.(type) {
	case string:
		p = MigrateKeyParams{PrivateKey: v}
	case MigrateKeyParams:
		p = v
	default:
		return nil, fmt.Errorf("invalid migration params type: %T", params)
	}
	if err := c.Call(MethodMigrateLegacyKey, p, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// StopService requests a graceful service stop
func (c *Client) StopService() (*StopServiceResult, error) {
	var res StopServiceResult
	if err := c.Call(MethodStopService, nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
