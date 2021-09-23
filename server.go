package redhub

import (
	"bytes"
	"sync"

	"github.com/IceFireDB/redhub/pkg/resp"
	"github.com/panjf2000/gnet"
)

const (
	// None indicates that no action should occur following an event.
	None Action = iota

	// Close closes the connection.
	Close

	// Shutdown shutdowns the server.
	Shutdown
)

var iceConn map[gnet.Conn]*connBuffer
var connSync sync.RWMutex

type Conn struct {
	gnet.Conn
}

type Action int
type Options struct {
	gnet.Options
}

type redisServer struct {
	*gnet.EventServer
	onOpened func(c *Conn) (out []byte, action Action)
	onClosed func(c *Conn, err error) (action Action)
	handler  func(cmd resp.Command, out []byte) ([]byte, Action)
}

type connBuffer struct {
	buf     bytes.Buffer
	command []resp.Command
}

func (rs *redisServer) OnOpened(c gnet.Conn) (out []byte, action gnet.Action) {
	connSync.Lock()
	defer connSync.Unlock()
	iceConn[c] = new(connBuffer)
	rs.onOpened(&Conn{Conn: c})
	return
}

func (rs *redisServer) OnClosed(c gnet.Conn, err error) (action gnet.Action) {
	connSync.Lock()
	defer connSync.Unlock()
	delete(iceConn, c)
	rs.onClosed(&Conn{Conn: c}, err)
	return
}

func (rs *redisServer) React(frame []byte, c gnet.Conn) (out []byte, action gnet.Action) {
	connSync.RLock()
	defer connSync.RUnlock()
	cb, ok := iceConn[c]
	if !ok {
		out = resp.AppendError(out, "ERR Client is closed")
		return
	}
	cb.buf.Write(frame)
	cmds, lastbyte, err := resp.ReadCommands(cb.buf.Bytes())
	if err != nil {
		out = resp.AppendError(out, "ERR "+err.Error())
		return
	}
	cb.command = append(cb.command, cmds...)
	cb.buf.Reset()
	if len(lastbyte) == 0 {
		var status Action
		for len(cb.command) > 0 {
			cmd := cb.command[0]
			if len(cb.command) == 1 {
				cb.command = nil
			} else {
				cb.command = cb.command[1:]
			}
			out, status = rs.handler(cmd, out)
			switch status {
			case Close:
				action = gnet.Close
			}
		}
	} else {
		cb.buf.Write(lastbyte)
	}
	return
}

func init() {
	iceConn = make(map[gnet.Conn]*connBuffer)
}

func ListendAndServe(addr string,
	options Options,
	onOpened func(c *Conn) (out []byte, action Action),
	onClosed func(c *Conn, err error) (action Action),
	handler func(cmd resp.Command, out []byte) ([]byte, Action),
) error {
	rs := &redisServer{
		onOpened: onOpened,
		onClosed: onClosed,
		handler:  handler,
	}
	return gnet.Serve(rs, addr, gnet.WithOptions(options.Options))
}
