package wire

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
)

const defaultMaxMsgLength int64 = 512 * 1024

var (
	ErrMsgType      = errors.New("message type error")
	ErrMaxMsgLength = errors.New("message length exceed the limit")
	ErrMsgLength    = errors.New("message length error")
	ErrMsgFormat    = errors.New("message format error")

	defaultMapper = NewMsgTransportMapper()
)

// MsgTransportMapper maps control-stream bytes to Message values.
type MsgTransportMapper struct {
	typeMap      map[byte]reflect.Type
	typeByteMap  map[reflect.Type]byte
	maxMsgLength int64
}

// NewMsgTransportMapper registers the full wire type table.
func NewMsgTransportMapper() *MsgTransportMapper {
	m := &MsgTransportMapper{
		typeMap:      make(map[byte]reflect.Type),
		typeByteMap:  make(map[reflect.Type]byte),
		maxMsgLength: defaultMaxMsgLength,
	}
	m.registerAll()
	return m
}

func (m *MsgTransportMapper) registerAll() {
	m.registerMsg(TypeClientHello, ClientHello{})
	m.registerMsg(TypeServerHello, ServerHello{})
	m.registerMsg(TypeReqWorkConn, ReqWorkConn{})
	m.registerMsg(TypeNewWorkConn, NewWorkConn{})
	m.registerMsg(TypeStartWorkConn, StartWorkConn{})
	m.registerMsg(TypeNewEndpoint, NewEndpoint{})
	m.registerMsg(TypeCloseEndpoint, CloseEndpoint{})
	m.registerMsg(TypeEndpointHealth, EndpointHealth{})
	m.registerMsg(TypeDiscoverTools, DiscoverTools{})
	m.registerMsg(TypeDiscoverToolsResult, DiscoverToolsResult{})
	m.registerMsg(TypeDisconnect, Disconnect{})
}

func (m *MsgTransportMapper) registerMsg(typeByte byte, prototype Message) {
	t := reflect.TypeOf(prototype)
	m.typeMap[typeByte] = t
	m.typeByteMap[t] = typeByte
}

// ReadMsg decodes one framed message from r.
func ReadMsg(r io.Reader) (Message, error) {
	return defaultMapper.Read(r)
}

// WriteMsg encodes one framed message to w.
func WriteMsg(w io.Writer, m Message) error {
	return defaultMapper.Write(w, m)
}

// Read decodes one framed message from r.
func (m *MsgTransportMapper) Read(r io.Reader) (Message, error) {
	typeByte, payload, err := m.readFrame(r)
	if err != nil {
		return nil, err
	}
	return m.decode(typeByte, payload)
}

// Write encodes one framed message to w.
func (m *MsgTransportMapper) Write(w io.Writer, message Message) error {
	frame, err := m.encode(message)
	if err != nil {
		return err
	}
	_, err = w.Write(frame)
	return err
}

func (m *MsgTransportMapper) readFrame(r io.Reader) (typeByte byte, payload []byte, err error) {
	if err = binary.Read(r, binary.BigEndian, &typeByte); err != nil {
		return 0, nil, fmt.Errorf("read msg type: %w", err)
	}
	if _, ok := m.typeMap[typeByte]; !ok {
		return 0, nil, ErrMsgType
	}

	var length int64
	if err = binary.Read(r, binary.BigEndian, &length); err != nil {
		return 0, nil, fmt.Errorf("read msg length: %w", err)
	}
	if length > m.maxMsgLength {
		return 0, nil, ErrMaxMsgLength
	}
	if length < 0 {
		return 0, nil, ErrMsgLength
	}

	payload = make([]byte, length)
	n, err := io.ReadFull(r, payload)
	if err != nil {
		return 0, nil, fmt.Errorf("read msg payload: %w", err)
	}
	if int64(n) != length {
		return 0, nil, ErrMsgFormat
	}
	return typeByte, payload, nil
}

func (m *MsgTransportMapper) decode(typeByte byte, payload []byte) (Message, error) {
	t, ok := m.typeMap[typeByte]
	if !ok {
		return nil, ErrMsgType
	}
	message := reflect.New(t).Interface().(Message)
	if len(payload) == 0 {
		return message, nil
	}
	if err := json.Unmarshal(payload, message); err != nil {
		msgType, _ := MessageTypeFromByte(typeByte)
		return nil, fmt.Errorf("decode msg %q: %w", msgType, err)
	}
	return message, nil
}

func (m *MsgTransportMapper) encode(message Message) ([]byte, error) {
	typeByte, err := m.typeByteFor(message)
	if err != nil {
		return nil, err
	}
	content, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("marshal msg: %w", err)
	}
	buf := bytes.NewBuffer(make([]byte, 0, 1+8+len(content)))
	if err := buf.WriteByte(typeByte); err != nil {
		return nil, err
	}
	if err := binary.Write(buf, binary.BigEndian, int64(len(content))); err != nil {
		return nil, err
	}
	if _, err := buf.Write(content); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (m *MsgTransportMapper) typeByteFor(message Message) (byte, error) {
	t := reflect.TypeOf(message)
	if t == nil {
		return 0, fmt.Errorf("nil message")
	}
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	typeByte, ok := m.typeByteMap[t]
	if !ok {
		return 0, fmt.Errorf("unknown message type %s", t.String())
	}
	return typeByte, nil
}
