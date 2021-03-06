package xmpp

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"html/template"
	"net/url"
	"strings"
	"time"

	"github.com/superp00t/etc/yo"
	"github.com/superp00t/go-web-daemons/ws"
)

const (
	OpenStanza          = "<open xmlns='urn:ietf:params:xml:ns:xmpp-framing' to='{{.Host}}' version='1.0'/>"
	AuthStanza          = "<auth xmlns='urn:ietf:params:xml:ns:xmpp-sasl' mechanism='ANONYMOUS'/>"
	BindStanza          = "<iq type='set' id='_bind_auth_2' xmlns='jabber:client'><bind xmlns='urn:ietf:params:xml:ns:xmpp-bind'/></iq>"
	SessStanza          = "<iq type='set' id='_session_auth_2' xmlns='jabber:client'><session xmlns='urn:ietf:params:xml:ns:xmpp-session'/></iq>"
	JoinMucStanza       = "<presence from='{{.JID}}' to='{{.MUCJID}}' xmlns='jabber:client'><x xmlns='http://jabber.org/protocol/muc'/></presence>"
	JoinMucStanza2      = "<presence from='{{.JID}}' to='{{.MUCJID}}' xmlns='jabber:client'><show/><status/></presence>"
	SendMessageStanza   = "<message to='{{.Recipient}}' from='{{.JID}}' type='{{.Type}}' xmlns='jabber:client'><body xmlns='jabber:client'>{{.Body}}</body><x xmlns='jabber:x:event'><active/></x></message>"
	SendComposingStanza = "<message to='{{.Recipient}}' from='{{.JID}}' type='{{.Type}}' id='composing' xmlns='jabber:client'><body/><x xmlns='jabber:x:event'><composing xmlns='http://jabber.org/protocol/chatstates'/></x></message>"
	SendPausedStanza    = "<message to='{{.Recipient}}' from='{{.JID}}' type='{{.Type}}' id='paused' xmlns='jabber:client'><body/><x xmlns='jabber:x:event'><paused xmlns='http://jabber.org/protocol/chatstates'/></x></message>"
	KickUserStanza      = `<iq from='{{.JID}}' id='kick1' to='{{.MUCJID}}' type='set'><query xmlns='http://jabber.org/protocol/muc#admin'><item nick='{{.Nick}}' role='none'><reason>{{.Reason}}</reason></item></query></iq>`
	PingResponse        = `<iq type='result' to='{{.Host}}' id='{{.Id}}' xmlns='jabber:client'/>`
)

var (
	RateLimited = errors.New("xmpp: rate limited")
)

type Presence struct {
	Presence xml.Name
	From     string `xml:"from,attr"`
	To       string `xml:"to,attr"`
	Type     string `xml:"type,attr"`
	Error    *Error `xml:"error"`
}

type Message struct {
	Message xml.Name
	Type    string `xml:"type,attr"`
	From    string `xml:"from,attr"`
	To      string `xml:"to,attr"`
	Id      string `xml:"id,attr"`
	Body    string `xml:"body"`
	Error   Error  `xml:"error"`
	X       Event  `xml:"x"`
}

type Error struct {
	Code int    `xml:"code"`
	Text string `xml:"text"`
}
type Event struct {
	Composing *string `xml:"composing"`
	Paused    *string `xml:"paused"`
}

type Stanza struct {
	Host      string
	Id        string
	JID       string
	MUCJID    string
	Recipient string
	Type      string
	Body      string
	Nick      string
	Reason    string
	MyNick    string
}

type IQ struct {
	XMLName xml.Name
	Id      string `xml:"id,attr"`
	Type    string `xml:"type,attr"`
	Bind    Bind   `xml:"bind"`
	Ping    *Ping  `xml:"ping"`
}

type Ping struct{}

type Bind struct {
	XMLName xml.Name
	JID     string `xml:"jid"`
}

type Opts struct {
	Debug              bool
	URL                string
	Host               string
	Username, Password string
	Proxy              string
}

type Conn struct {
	JID      string
	Opts     Opts
	socket   *ws.Conn
	lastRecv time.Time
}

func (c *Conn) Disconnect() {
	if c == nil {
		return
	}
	c.socket.Send([]byte(`<close xmlns="urn:ietf:params:xml:ns:xmpp-framing" />`))
	c.socket.Close()
}

func (s Stanza) Render(st string) string {
	t, err := template.New("stanza").Parse(st)
	if err != nil {
		panic(err)
	}

	var bf bytes.Buffer
	err = t.Execute(&bf, s)
	if err != nil {
		panic(err)
	}

	return bf.String()
}

func Dial(o Opts) (*Conn, error) {
	u, err := url.Parse(o.URL)
	if err != nil {
		return nil, err
	}

	u.Scheme = "https"
	u.Path = "/"

	c, err := ws.DialOpts(ws.Opts{
		URL:         o.URL,
		Subprotocol: "xmpp",
		Origin:      "https://cryptodog.github.io/",
		UserAgent:   "Mozilla/5.0 (Windows NT 6.1; rv:31.0) Gecko/20100101 Firefox/31.0",
		Socks5:      o.Proxy,
	})

	if err != nil {
		return nil, err
	}

	cli := &Conn{
		socket:   c,
		Opts:     o,
		lastRecv: time.Now(),
	}

	if o.Username == "" {
		cli.send(Stanza{Host: o.Host}.Render(OpenStanza))
	} else {
		return nil, fmt.Errorf("Authenticated login not yet implemented")
	}

	cli.recv()
	cli.recv()
	cli.send(AuthStanza)
	cli.recv()
	cli.send(Stanza{Host: o.Host}.Render(OpenStanza))
	cli.recv()
	cli.recv()
	cli.send(BindStanza)

	str, _ := cli.recv()
	i, _ := ParseIQ(str)

	cli.JID = i.Bind.JID
	cli.send(SessStanza)
	cli.recv()

	return cli, nil
}

func (c *Conn) send(stanza string) error {
	if c.Opts.Debug {
		PrintTree(stanza)
	}
	err := c.socket.Send([]byte(stanza))
	if err != nil {
		yo.Warn(err)
		return err
	}

	return nil
}

func (c *Conn) recv() (string, error) {
start:
	_stanza, err := c.socket.Recv()
	if err != nil {
		return "", err
	}

	if len(_stanza) > 75000 {
		if c.Opts.Debug {
			fmt.Println("Stanza length", len(_stanza), "time", time.Since(c.lastRecv))
		}
		c.lastRecv = time.Now()
		goto start
	}

	stanza := string(_stanza)

	if c.Opts.Debug {
		PrintTree(stanza)
	}

	return stanza, nil
}

func (c *Conn) JoinMUC(room, conference, nick string) {
	mjid := JID{
		Local: room,
		Host:  conference,
		Node:  nick,
	}
	c.send(Stanza{JID: c.JID, MUCJID: mjid.String()}.Render(JoinMucStanza))
	c.send(Stanza{JID: c.JID, MUCJID: mjid.String()}.Render(JoinMucStanza2))
}

type NicknameInUse struct {
	JID
}

func (c *Conn) Recv() (interface{}, error) {
rcv:
	str, err := c.recv()
	if err != nil {
		return nil, err
	}

	if strings.HasPrefix(str, "<presence") {
		pres, err := ParsePresence(str)
		if err != nil {
			return nil, err
		}

		if pres.Type == "error" {
			if pres.Error.Code == 409 {
				yo.L(4).Warn(str)
				j, _ := ParseJID(pres.From)
				return NicknameInUse{j}, nil
			}
			return nil, fmt.Errorf("%d %s", pres.Error.Code, pres.Error.Text)
		}
		return pres, nil
	}

	if strings.HasPrefix(str, "<message") {
		msg, err := ParseMessage(str)
		if err != nil {
			return nil, err
		}

		if msg.Type == "error" {
			if msg.Error.Text == "Traffic rate limit is exceeded" {
				return "", RateLimited
			}
			return nil, errors.New(msg.Error.Text)
		}
		return msg, nil
	}

	if strings.HasPrefix(str, "<iq") {
		var iq IQ
		iq.XMLName = xml.Name{Local: "iq", Space: "jabber:client"}
		xml.Unmarshal([]byte(str), &iq)
		if iq.Ping != nil {
			c.send(Stanza{
				Host: c.Opts.Host,
				Id:   iq.Id,
			}.Render(PingResponse))
			goto rcv
		}
		return iq, nil
	}

	return nil, fmt.Errorf("Unknown thing %s", str)
}

func (c *Conn) SendMessage(jid, typeof, body string) error {
	if c == nil {
		return fmt.Errorf("xmpp: conn is nil")
	}

	return c.send(Stanza{
		Recipient: jid,
		Type:      typeof,
		Body:      body,
		JID:       c.JID,
	}.Render(SendMessageStanza))
}

func (c *Conn) SendComposing(jid, typeof string) error {
	if c == nil {
		return fmt.Errorf("xmpp: conn is nil")
	}

	return c.send(Stanza{
		Recipient: jid,
		Type:      typeof,
		JID:       c.JID,
	}.Render(SendComposingStanza))
}

func (c *Conn) SendPaused(jid, typeof string) error {
	return c.send(Stanza{
		Recipient: jid,
		Type:      typeof,
		JID:       c.JID,
	}.Render(SendPausedStanza))
}

func (c *Conn) Kick(lobby, nick, reason string) error {
	c.send(Stanza{
		JID:    c.JID,
		MUCJID: lobby,
		Nick:   nick,
		Reason: reason,
	}.Render(KickUserStanza))
	return nil
}
