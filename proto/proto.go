package proto

import (
	"encoding/json"
	"fmt"
)

type Type uint8

const (
	TypeJoinMessage    Type = 'j'
	TypeLeaveMessage   Type = 'l'
	TypeGroupMessage   Type = 'g'
	TypePrivateMessage Type = 'p'
	TypeRosterMessage  Type = 'r'
	TypeErrorMessage   Type = 'e'
)

func (t Type) String() string {
	switch t {
	case TypeJoinMessage:
		return "JoinMessage"
	case TypeLeaveMessage:
		return "LeaveMessage"
	case TypeGroupMessage:
		return "GroupMessage"
	case TypePrivateMessage:
		return "PrivateMessage"
	case TypeRosterMessage:
		return "RosterMessage"
	case TypeErrorMessage:
		return "ErrorMessage"
	default:
		return fmt.Sprintf("Unknown (0x02X)", t)
	}
}

// Generic message type; requires manual unpacking of Raw field into a SpecificMessage identified by Type field.
type Message struct {
	Type Type
	Raw  json.RawMessage
}

func (msg *Message) String() string {
	return string(msg.Type) + string(msg.Raw)
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
