/**********************************************************\
|                                                          |
|                          hprose                          |
|                                                          |
| Official WebSite: http://www.hprose.com/                 |
|                   http://www.hprose.net/                 |
|                   http://www.hprose.org/                 |
|                                                          |
\**********************************************************/
/**********************************************************\
 *                                                        *
 * hprose/tcp_client.go                                   *
 *                                                        *
 * hprose tcp client for Go.                              *
 *                                                        *
 * LastModified: Feb 25, 2014                             *
 * Author: Ma Bingyao <andot@hprose.com>                  *
 *                                                        *
\**********************************************************/

package hprose

import (
	"crypto/tls"
	"net"
	"net/url"
	"sync"
	"time"
)

type TcpClient struct {
	*BaseClient
	deadline        interface{}
	keepAlive       interface{}
	keepAlivePeriod interface{}
	linger          interface{}
	noDelay         interface{}
	readBuffer      interface{}
	readDeadline    interface{}
	writerBuffer    interface{}
	writerDeadline  interface{}
	config          *tls.Config
}

type tcpConnStatus int

const (
	free = tcpConnStatus(iota)
	using
	closing
)

type TcpConnEntry struct {
	uri    string
	conn   net.Conn
	status tcpConnStatus
}

func (connEntry *TcpConnEntry) Get() net.Conn {
	return connEntry.conn
}

func (connEntry *TcpConnEntry) Set(conn net.Conn) {
	if conn != nil {
		connEntry.conn = conn
	}
}

func (connEntry *TcpConnEntry) Close() {
	connEntry.status = closing
}

type TcpConnPool struct {
	sync.Mutex
	pool []*TcpConnEntry
}

func (connPool *TcpConnPool) Get(uri string) *TcpConnEntry {
	connPool.Lock()
	defer connPool.Unlock()
	for _, entry := range connPool.pool {
		if entry.status == free {
			if entry.uri == uri {
				entry.status = using
				return entry
			} else if entry.uri == "" {
				entry.status = using
				entry.uri = uri
				return entry
			}
		}
	}
	entry := &TcpConnEntry{uri, nil, using}
	connPool.pool = append(connPool.pool, entry)
	return entry
}

func freeConns(conns []net.Conn) {
	for _, conn := range conns {
		conn.Close()
	}
}

func (connPool *TcpConnPool) Close(uri string) {
	connPool.Lock()
	defer connPool.Unlock()
	conns := make([]net.Conn, 0, len(connPool.pool))
	for _, entry := range connPool.pool {
		if entry.uri == uri {
			if entry.status == free {
				conns = append(conns, entry.conn)
				entry.conn = nil
				entry.uri = ""
			} else {
				entry.Close()
			}
		}
	}
	go freeConns(conns)
}

func (connPool *TcpConnPool) Free(entry *TcpConnEntry) {
	if entry.status == closing {
		if entry.conn != nil {
			go entry.conn.Close()
			entry.conn = nil
		}
		entry.uri = ""
	}
	entry.status = free
}

type TcpTransporter struct {
	connPool *TcpConnPool
	*TcpClient
}

func NewTcpClient(uri string) Client {
	trans := &TcpTransporter{connPool: &TcpConnPool{pool: make([]*TcpConnEntry, 0)}}
	client := &TcpClient{BaseClient: NewBaseClient(trans)}
	trans.TcpClient = client
	client.SetUri(uri)
	return client
}

func (client *TcpClient) SetUri(uri string) {
	if u, err := url.Parse(uri); err == nil {
		if u.Scheme != "tcp" && u.Scheme != "tcp4" && u.Scheme != "tcp6" {
			panic("This client desn't support " + u.Scheme + " scheme.")
		}
	}
	client.Close()
	client.BaseClient.SetUri(uri)
}

func (client *TcpClient) Close() {
	uri := client.Uri()
	if uri == "" {
		client.Transporter.(*TcpTransporter).connPool.Close(uri)
	}
}

func (client *TcpClient) SetDeadline(t time.Time) {
	client.deadline = t
}

func (client *TcpClient) SetKeepAlive(keepalive bool) {
	client.keepAlive = keepalive
}

func (client *TcpClient) SetKeepAlivePeriod(d time.Duration) {
	client.keepAlivePeriod = d
}

func (client *TcpClient) SetLinger(sec int) {
	client.linger = sec
}

func (client *TcpClient) SetNoDelay(noDelay bool) {
	client.noDelay = noDelay
}

func (client *TcpClient) SetReadBuffer(bytes int) {
	client.readBuffer = bytes
}

func (client *TcpClient) SetReadDeadline(t time.Time) {
	client.readDeadline = t
}

func (client *TcpClient) SetWriteBuffer(bytes int) {
	client.writerBuffer = bytes
}

func (client *TcpClient) SetWriteDeadline(t time.Time) {
	client.writerDeadline = t
}

func (client *TcpClient) SetTLSConfig(config *tls.Config) {
	client.config = config
}

func (t *TcpTransporter) SendAndReceive(uri string, odata []byte) (idata []byte, err error) {
	connEntry := t.connPool.Get(uri)
	defer func() {
		if err != nil {
			connEntry.Close()
			t.connPool.Free(connEntry)
		}
	}()
	conn := connEntry.Get()
	if conn == nil {
		var u *url.URL
		if u, err = url.Parse(uri); err != nil {
			return nil, err
		}
		var tcpaddr *net.TCPAddr
		if tcpaddr, err = net.ResolveTCPAddr(u.Scheme, u.Host); err != nil {
			return nil, err
		}
		if conn, err = net.DialTCP("tcp", nil, tcpaddr); err != nil {
			return nil, err
		}
		if t.keepAlive != nil {
			if err = conn.(*net.TCPConn).SetKeepAlive(t.keepAlive.(bool)); err != nil {
				return nil, err
			}
		}
		if t.keepAlivePeriod != nil {
			if kap, ok := conn.(iKeepAlivePeriod); ok {
				if err = kap.SetKeepAlivePeriod(t.keepAlivePeriod.(time.Duration)); err != nil {
					return nil, err
				}
			}
		}
		if t.linger != nil {
			if err = conn.(*net.TCPConn).SetLinger(t.linger.(int)); err != nil {
				return nil, err
			}
		}
		if t.noDelay != nil {
			if err = conn.(*net.TCPConn).SetNoDelay(t.noDelay.(bool)); err != nil {
				return nil, err
			}
		}
		if t.readBuffer != nil {
			if err = conn.(*net.TCPConn).SetReadBuffer(t.readBuffer.(int)); err != nil {
				return nil, err
			}
		}
		if t.writerBuffer != nil {
			if err = conn.(*net.TCPConn).SetWriteBuffer(t.writerBuffer.(int)); err != nil {
				return nil, err
			}
		}
		if t.deadline != nil {
			if err = conn.SetDeadline(t.deadline.(time.Time)); err != nil {
				return nil, err
			}
		}
		if t.readDeadline != nil {
			if err = conn.SetReadDeadline(t.readDeadline.(time.Time)); err != nil {
				return nil, err
			}
		}
		if t.writerDeadline != nil {
			if err = conn.SetWriteDeadline(t.writerDeadline.(time.Time)); err != nil {
				return nil, err
			}
		}
		if t.config != nil {
			conn = tls.Client(conn, t.config)
		}
		connEntry.Set(conn)
	}
	if err = sendDataOverTcp(conn, odata); err != nil {
		return nil, err
	}
	if idata, err = receiveDataOverTcp(conn); err != nil {
		return nil, err
	}
	t.connPool.Free(connEntry)
	return idata, nil
}
