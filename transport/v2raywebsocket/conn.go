package v2raywebsocket

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"os"
	"time"

	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing/common/buf"
	E "github.com/sagernet/sing/common/exceptions"
	"github.com/sagernet/websocket"
)

type WebsocketConn struct {
	*websocket.Conn
	*Writer
	remoteAddr net.Addr
	reader     io.Reader
}

func NewServerConn(wsConn *websocket.Conn, remoteAddr net.Addr) *WebsocketConn {
	return &WebsocketConn{
		Conn:       wsConn,
		remoteAddr: remoteAddr,
		Writer:     &Writer{wsConn, true},
	}
}

func (c *WebsocketConn) Close() error {
	err := c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(C.TCPTimeout))
	if err != nil {
		return c.Conn.Close()
	}
	return nil
}

func (c *WebsocketConn) Read(b []byte) (n int, err error) {
	for {
		if c.reader == nil {
			_, c.reader, err = c.NextReader()
			if err != nil {
				err = wrapError(err)
				return
			}
		}
		n, err = c.reader.Read(b)
		if E.IsMulti(err, io.EOF) {
			c.reader = nil
			continue
		}
		err = wrapError(err)
		return
	}
}

func (c *WebsocketConn) RemoteAddr() net.Addr {
	if c.remoteAddr != nil {
		return c.remoteAddr
	}
	return c.Conn.RemoteAddr()
}

func (c *WebsocketConn) SetDeadline(t time.Time) error {
	return os.ErrInvalid
}

func (c *WebsocketConn) FrontHeadroom() int {
	return frontHeadroom
}

type EarlyWebsocketConn struct {
	*Client
	ctx    context.Context
	conn   *WebsocketConn
	create chan struct{}
}

func (c *EarlyWebsocketConn) Read(b []byte) (n int, err error) {
	if c.conn == nil {
		<-c.create
	}
	return c.conn.Read(b)
}

func (c *EarlyWebsocketConn) Write(b []byte) (n int, err error) {
	if c.conn != nil {
		return c.conn.Write(b)
	}
	var (
		earlyData []byte
		lateData  []byte
		conn      *websocket.Conn
		response  *http.Response
	)
	if len(b) > int(c.maxEarlyData) {
		earlyData = b[:c.maxEarlyData]
		lateData = b[c.maxEarlyData:]
	} else {
		earlyData = b
	}
	if len(earlyData) > 0 {
		earlyDataString := base64.RawURLEncoding.EncodeToString(earlyData)
		if c.earlyDataHeaderName == "" {
			conn, response, err = c.dialer.DialContext(c.ctx, c.uri+earlyDataString, c.headers)
		} else {
			headers := c.headers.Clone()
			headers.Set(c.earlyDataHeaderName, earlyDataString)
			conn, response, err = c.dialer.DialContext(c.ctx, c.uri, headers)
		}
	} else {
		conn, response, err = c.dialer.DialContext(c.ctx, c.uri, c.headers)
	}
	if err != nil {
		return 0, wrapDialError(response, err)
	}
	c.conn = &WebsocketConn{Conn: conn, Writer: &Writer{conn, false}}
	close(c.create)
	if len(lateData) > 0 {
		_, err = c.conn.Write(lateData)
	}
	if err != nil {
		return
	}
	return len(b), nil
}

func (c *EarlyWebsocketConn) WriteBuffer(buffer *buf.Buffer) error {
	if c.conn != nil {
		return c.conn.WriteBuffer(buffer)
	}
	var (
		earlyData []byte
		lateData  []byte
		conn      *websocket.Conn
		response  *http.Response
		err       error
	)
	if buffer.Len() > int(c.maxEarlyData) {
		earlyData = buffer.Bytes()[:c.maxEarlyData]
		lateData = buffer.Bytes()[c.maxEarlyData:]
	} else {
		earlyData = buffer.Bytes()
	}
	if len(earlyData) > 0 {
		earlyDataString := base64.RawURLEncoding.EncodeToString(earlyData)
		if c.earlyDataHeaderName == "" {
			conn, response, err = c.dialer.DialContext(c.ctx, c.uri+earlyDataString, c.headers)
		} else {
			headers := c.headers.Clone()
			headers.Set(c.earlyDataHeaderName, earlyDataString)
			conn, response, err = c.dialer.DialContext(c.ctx, c.uri, headers)
		}
	} else {
		conn, response, err = c.dialer.DialContext(c.ctx, c.uri, c.headers)
	}
	if err != nil {
		return wrapDialError(response, err)
	}
	c.conn = &WebsocketConn{Conn: conn, Writer: &Writer{conn, false}}
	close(c.create)
	if len(lateData) > 0 {
		_, err = c.conn.Write(lateData)
	}
	return err
}

func (c *EarlyWebsocketConn) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

func (c *EarlyWebsocketConn) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

func (c *EarlyWebsocketConn) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

func (c *EarlyWebsocketConn) SetDeadline(t time.Time) error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.SetDeadline(t)
}

func (c *EarlyWebsocketConn) SetReadDeadline(t time.Time) error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.SetReadDeadline(t)
}

func (c *EarlyWebsocketConn) SetWriteDeadline(t time.Time) error {
	if c.conn == nil {
		return os.ErrInvalid
	}
	return c.conn.SetWriteDeadline(t)
}

func (c *EarlyWebsocketConn) FrontHeadroom() int {
	return frontHeadroom
}

func wrapError(err error) error {
	if websocket.IsCloseError(err, websocket.CloseNormalClosure) {
		return io.EOF
	}
	if websocket.IsCloseError(err, websocket.CloseAbnormalClosure) {
		return net.ErrClosed
	}
	return err
}
