package muxnative

import (
	"fmt"
	"net"
	"time"
)

// daemonClient communicates with the mux daemon over a Unix socket.
// Each method opens a fresh connection, sends one request, reads one response,
// and closes the connection — except Attach which keeps the connection open.
type daemonClient struct {
	sockPath string
}

func newDaemonClient(sockPath string) *daemonClient {
	return &daemonClient{sockPath: sockPath}
}

// do opens a connection, sends req, reads a response, and closes.
func (c *daemonClient) do(req Request) (Response, error) {
	conn, err := c.dial()
	if err != nil {
		return Response{}, err
	}
	defer conn.Close()

	if err := writeMsg(conn, req); err != nil {
		return Response{}, err
	}
	var resp Response
	if err := readMsg(conn, &resp); err != nil {
		return Response{}, err
	}
	if !resp.OK {
		return resp, fmt.Errorf("%s", resp.Error)
	}
	return resp, nil
}

// dial opens a new connection to the daemon socket.
func (c *daemonClient) dial() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.sockPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to mux daemon (%s): %w", c.sockPath, err)
	}
	conn.SetDeadline(time.Now().Add(10 * time.Second)) //nolint:errcheck
	return conn, nil
}

// dialRaw opens a connection without a read deadline (used for attach).
func (c *daemonClient) dialRaw() (net.Conn, error) {
	conn, err := net.DialTimeout("unix", c.sockPath, 2*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to mux daemon (%s): %w", c.sockPath, err)
	}
	return conn, nil
}
