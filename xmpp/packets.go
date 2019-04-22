package xmpp

import (
	"encoding/xml"

	"github.com/beevik/etree"
)

func ParseIQ(data string) (IQ, error) {
	var i IQ
	i.XMLName = xml.Name{Local: "iq", Space: "jabber:client"}

	err := xml.Unmarshal([]byte(data), &i)
	return i, err
}

func ParseMessage(data string) (Message, error) {
	var msg Message
	msg.Message = xml.Name{Local: "message", Space: "jabber:client"}
	err := xml.Unmarshal([]byte(data), &msg)
	return msg, err
}

func ParsePresence(data string) (Presence, error) {
	var pres Presence
	pres.Presence = xml.Name{Local: "presence", Space: "jabber:client"}
	err := xml.Unmarshal([]byte(data), &pres)
	return pres, err
}

func Parse(data string) (interface{}, error) {
	doc := etree.NewDocument()
	err := doc.ReadFromString(data)
	if err != nil {
		return nil, err
	}

	root := doc.Root()

	switch root.Tag {
	case "iq":
		return ParseIQ(data)
	case "message":
		return ParseMessage(data)
	case "presence":
		return ParsePresence(data)
	default:
		return nil, nil
	}
}
