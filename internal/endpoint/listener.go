package endpoint

import (
	"fmt"
	"net"
	"sync"
	"time"
)

const (
	listenAcceptBuffer = 16
	listenOfferWait    = 2 * time.Second
)

type endpointAddr struct {
	endpointID string
}

func (a endpointAddr) Network() string { return "orbitproxy" }
func (a endpointAddr) String() string  { return "orbitproxy:" + a.endpointID }

// chanListener is a net.Listener fed by inbound work connections (Listen mode).
type chanListener struct {
	endpointID string
	ch         chan net.Conn
	closed     chan struct{}
	closeOnce  sync.Once
	addr       net.Addr
}

func newChanListener(endpointID string) *chanListener {
	return &chanListener{
		endpointID: endpointID,
		ch:         make(chan net.Conn, listenAcceptBuffer),
		closed:     make(chan struct{}),
		addr:       endpointAddr{endpointID: endpointID},
	}
}

func (l *chanListener) Accept() (net.Conn, error) {
	select {
	case <-l.closed:
		return nil, net.ErrClosed
	case c, ok := <-l.ch:
		if !ok {
			return nil, net.ErrClosed
		}
		return c, nil
	}
}

func (l *chanListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
		for {
			select {
			case c := <-l.ch:
				if c != nil {
					_ = c.Close()
				}
			default:
				return
			}
		}
	})
	return nil
}

func (l *chanListener) Addr() net.Addr { return l.addr }

// Offer delivers a work connection to Accept. Returns false if the listener is
// closed or the accept side does not take the conn within listenOfferWait.
func (l *chanListener) Offer(c net.Conn) bool {
	if c == nil {
		return false
	}
	select {
	case <-l.closed:
		return false
	default:
	}

	select {
	case <-l.closed:
		return false
	case l.ch <- c:
		return true
	default:
	}

	timer := time.NewTimer(listenOfferWait)
	defer timer.Stop()
	select {
	case <-l.closed:
		return false
	case l.ch <- c:
		return true
	case <-timer.C:
		return false
	}
}

func (l *chanListener) String() string {
	return fmt.Sprintf("orbitproxy-listener(%s)", l.endpointID)
}
