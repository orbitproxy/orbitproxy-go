package dispatcher

import (
	"io"
	"reflect"
	"sync"

	"github.com/orbitproxy/orbitproxy-go/wire"
)

// AsyncHandler runs the handler in a new goroutine so readLoop is not blocked.
func AsyncHandler(f func(wire.Message)) func(wire.Message) {
	return func(m wire.Message) {
		go f(m)
	}
}

// Dispatcher serializes writes and dispatches reads on a control connection.
type Dispatcher struct {
	rw io.ReadWriter

	sendCh         chan wire.Message
	doneCh         chan struct{}
	closeOnce      sync.Once
	msgHandlers    map[reflect.Type]func(wire.Message)
	defaultHandler func(wire.Message)
}

// NewDispatcher creates a dispatcher over rw.
func NewDispatcher(rw io.ReadWriter) *Dispatcher {
	return &Dispatcher{
		rw:          rw,
		sendCh:      make(chan wire.Message, 100),
		doneCh:      make(chan struct{}),
		msgHandlers: make(map[reflect.Type]func(wire.Message)),
	}
}

// Run starts send and read loops.
func (d *Dispatcher) Run() {
	go d.sendLoop()
	go d.readLoop()
}

func (d *Dispatcher) sendLoop() {
	for {
		select {
		case <-d.doneCh:
			return
		case m := <-d.sendCh:
			if err := wire.WriteMsg(d.rw, m); err != nil {
				d.closeDone()
				return
			}
		}
	}
}

func (d *Dispatcher) readLoop() {
	for {
		m, err := wire.ReadMsg(d.rw)
		if err != nil {
			d.closeDone()
			return
		}

		if handler, ok := d.msgHandlers[reflect.TypeOf(m)]; ok {
			handler(m)
		} else if d.defaultHandler != nil {
			d.defaultHandler(m)
		}
	}
}

// Send enqueues a message for writing.
func (d *Dispatcher) Send(m wire.Message) error {
	// Prefer the done path when both are ready so callers observe EOF after shutdown.
	select {
	case <-d.doneCh:
		return io.EOF
	default:
	}
	select {
	case <-d.doneCh:
		return io.EOF
	case d.sendCh <- m:
		select {
		case <-d.doneCh:
			return io.EOF
		default:
			return nil
		}
	}
}

// RegisterHandler registers a typed handler.
func (d *Dispatcher) RegisterHandler(prototype wire.Message, handler func(wire.Message)) {
	d.msgHandlers[reflect.TypeOf(prototype)] = handler
}

// SetDefaultHandler sets the fallback handler.
func (d *Dispatcher) SetDefaultHandler(handler func(wire.Message)) {
	d.defaultHandler = handler
}

// Done is closed when the dispatcher stops.
func (d *Dispatcher) Done() <-chan struct{} {
	return d.doneCh
}

func (d *Dispatcher) closeDone() {
	d.closeOnce.Do(func() { close(d.doneCh) })
}
