package proto

import (
	"encoding/json"
)

const (
	TypeJoinMessage    byte = 'j'
	TypeLeaveMessage   byte = 'l'
	TypeGroupMessage   byte = 'g'
	TypePrivateMessage byte = 'p'
	TypeRosterMessage  byte = 'r'
	TypeErrorMessage   byte = 'e'
)

// Generic message type; requires manual unpacking of Raw field into a SpecificMessage identified by Type field.
type Message struct {
	Type byte
	Raw  json.RawMessage
}

// Serialize a packed Message for network transfer.
func (msg *Message) Bytes() []byte {
	return append([]byte{msg.Type}, msg.Raw...)
}

type SpecificMessage interface {
	// This function packs the SpecificMessage into a Message and populates the Type field appropriately.
	Pack() *Message
}

// Pointers to the following types satisfy SpecificMessage.
type JoinMessage struct {
	Name string `json:"name"`
	Room string `json:"room,omitempty"`
}

type LeaveMessage struct {
	Name string `json:"name,omitempty"`
}

type GroupMessage struct {
	From string `json:"name,omitempty"`
	Text string `json:"text"`
}

type PrivateMessage struct {
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	Text string `json:"text"`
}

type RosterMessage struct {
	Users []string `json:"users"`
}

type ErrorMessage struct {
	Error string `json:"error"`
}

func (msg *JoinMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypeJoinMessage,
		Raw:  b,
	}
}

func (msg *LeaveMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypeLeaveMessage,
		Raw:  b,
	}
}

func (msg *GroupMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypeGroupMessage,
		Raw:  b,
	}
}

func (msg *PrivateMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypePrivateMessage,
		Raw:  b,
	}
}

func (msg *RosterMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypeRosterMessage,
		Raw:  b,
	}
}

func (msg *ErrorMessage) Pack() *Message {
	b, err := json.Marshal(msg)
	check(err)
	return &Message{
		Type: TypeErrorMessage,
		Raw:  b,
	}
}

func check(err error) {
	if err != nil {
		panic(err)
	}
}
