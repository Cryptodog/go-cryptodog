package dog

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Cryptodog/go-cryptodog/multiparty"
	"github.com/Cryptodog/go-cryptodog/xmpp"
	"github.com/coyim/otr3"
	"github.com/superp00t/etc/yo"
)

type Room struct {
	Name    string
	MyName  string
	Mp      *multiparty.Me
	Members map[string]*Member

	ml          *sync.Mutex
	killed      bool
	c           *Conn
	bexAddTout  chan float64
	bexTx       chan []BEX
	joinedEvent bool
}

type Member struct {
	r        *Room
	nickname string
	otr      *otr3.Conversation
	onInit   func()
}

func (m *Member) Name() string {
	return m.nickname
}

func (c *Conn) GetRoom(name string) *Room {
	c.rl.Lock()
	room := c.rooms[name]
	c.rl.Unlock()
	return room
}

func (c *Conn) GM(room string, body string) {
	if !utf8.ValidString(body) {
		return
	}

	c.Group(room, []byte(body))
}

func (c *Conn) GMf(room string, format string, args ...interface{}) {
	c.GM(room, fmt.Sprintf(format, args...))
}

func (c *Conn) Group(room string, b []byte) {
	if r := c.GetRoom(room); r != nil {
		r.Group(b)
	}
}

func (r *Room) GM(body string) {
	r.Group([]byte(body))
}

func (r *Room) Group(b []byte) {
	r.Mp.SendMessage(b)
}

func (r *Room) DM(user, data string) {
	r.GetMember(user).DM(data)
}

func (r *Room) GetMember(s string) *Member {
	if r == nil {
		return nil
	}
	r.ml.Lock()
	mem := r.Members[s]
	r.ml.Unlock()
	return mem
}

func (m *Member) DM(data string) {
	if m == nil {
		return
	}

	if m.otr == nil {
		m.initOtr()
	}

	vm, err := m.otr.Send([]byte(data))
	if err != nil {
		yo.L(4).Warn(err)
	}

	for _, v := range vm {
		m.r.c.c.SendMessage(xmpp.JID{
			Local: m.r.Name,
			Host:  m.r.c.Conference,
			Node:  m.nickname,
		}.String(), "chat", string(v))
	}
}

func (m *Member) initOtr() {
	if m.r.c.opt(DMDisabled) {
		return
	}

	if m.otr == nil {
		key := new(otr3.DSAPrivateKey)
		b64, err := base64.StdEncoding.DecodeString(m.r.c.loadString("otr"))
		if err != nil {
			panic(err)
		}
		_, ok := key.Parse(b64)
		if !ok {
			panic("malformed OTR key")
		}

		m.otr = new(otr3.Conversation)
		m.otr.SetOurKeys([]otr3.PrivateKey{key})
		m.otr.Rand = rand.Reader
		m.otr.SetSMPEventHandler(m)
		m.otr.SetMessageEventHandler(m)
		m.otr.SetSecurityEventHandler(m)
		m.otr.Policies.RequireEncryption()
		m.otr.Policies.AllowV2()
		m.otr.Policies.AllowV3()
	}
}

func (r *Room) Destroy() {
	r.killed = true
}

func (r *Room) emit(e Event) {
	e.Room = r.Name
	r.c.emit(e)
}

func (r *Room) transmitMp(b []byte) {
	r.c.c.SendMessage(
		xmpp.JID{
			Local: r.Name,
			Host:  r.c.Conference,
		}.String(),
		"groupchat",
		string(b),
	)
}

func (m *Member) HandleSecurityEvent(event otr3.SecurityEvent) {
	switch event {
	case otr3.GoneSecure:
		if m.onInit != nil {
			fn := m.onInit
			m.onInit = nil
			go fn()
		}
	}
}

func (m *Member) HandleMessageEvent(event otr3.MessageEvent, message []byte, err error, trace ...interface{}) {
	// yo.Spew(event)
	// yo.Spew(message)
}

func (m *Member) HandleSMPEvent(event otr3.SMPEvent, progressPercent int, question string) {
	switch event {
	case otr3.SMPEventAskForAnswer:
		m.r.emit(Event{
			Type: SMPQuestion,
			User: m.nickname,
			Body: question,
		})

	case otr3.SMPEventSuccess:
		m.r.emit(Event{
			Type: SMPSuccess,
			User: m.nickname,
			Body: question,
		})

	case otr3.SMPEventFailure:
		m.r.emit(Event{
			Type: SMPFailure,
			User: m.nickname,
			Body: question,
		})
	}
}

func (m *Member) Answer(data string) {
	if m == nil {
		return
	}
	if m.otr != nil {
		pdata := m.prepareAnswer(data, false)

		ts, err := m.otr.ProvideAuthenticationSecret([]byte(pdata))
		if err == nil {
			for _, v := range ts {
				m.direct(string(v))
			}
		}
	}
}

func (m *Member) prepareAnswer(answer string, ask bool) string {
	buddyMpFingerprint := m.r.Mp.Fingerprint(m.nickname)

	first := ""
	second := ""
	answer = strings.ToLower(answer)
	for _, v := range []rune(".,'\";?!") {
		answer = strings.Replace(answer, string(v), "", -1)
	}

	mee := m.r.Mp.Fingerprint("")

	if buddyMpFingerprint != "" {
		if ask {
			first = mee
		} else {
			first = buddyMpFingerprint
		}

		if ask {
			second = buddyMpFingerprint
		} else {
			second = mee
		}

		answer += ";" + first + ";" + second
	}

	return answer
}

func (m *Member) Ask(question, answer string) {
	if m == nil {
		return
	}

	doAsk := func() {
		ans := m.prepareAnswer(answer, true)
		ts, err := m.otr.StartAuthenticate(question, []byte(ans))
		if err == nil {
			for _, v := range ts {
				m.direct(string(v))
			}
		} else {
			yo.Warn(err)
		}
	}

	init := m.otr != nil && m.otr.IsEncrypted()

	if !init {
		yo.Warn(m.nickname, "isn't connected. adding to handler")
		m.onInit = doAsk
		m.initOtr()
		msg, err := m.otr.Send(otr3.ValidMessage{})
		if err == nil {
			for _, v := range msg {
				m.direct(string(v))
			}
		} else {
			yo.Warn(err)
		}
	} else {
		yo.Println(m.nickname, "is already connected")
		doAsk()
	}
}

func (m *Member) direct(str string) {
	m.r.c.c.SendMessage(
		xmpp.JID{
			Local: m.r.Name,
			Host:  m.r.c.Conference,
			Node:  m.nickname,
		}.String(),
		"chat",
		str,
	)
}

// Sends group composing message via Binary Extensions.
func (m *Room) SendGroupComposing() {
	if m == nil {
		return
	}

	m.SendBEXGroup([]BEX{
		{Header: BEX_COMPOSING},
	})
}

// Sends group paused message via Binary Extensions.
func (m *Room) SendGroupPaused() {
	if m == nil {
		return
	}

	m.SendBEXGroup([]BEX{
		{Header: BEX_PAUSED},
	})
}

// Sends private composing message via Binary Extensions
func (m *Room) SendPrivateComposing(target string) {
	if m == nil {
		return
	}

	m.SendBEXPrivate(target, []BEX{
		{Header: BEX_COMPOSING},
	})
}

// Sends private paused message via Binary Extensions
func (m *Room) SendPrivatePaused(target string) {
	if m == nil {
		return
	}

	m.SendBEXPrivate(target, []BEX{
		{Header: BEX_PAUSED},
	})
}

// Sends composing message via XMPP.
func (m *Room) SendXGroupComposing() {
	if m == nil {
		yo.Warn("room is nil")
		return
	}
	m.c.c.SendComposing(xmpp.JID{
		Local: m.Name,
		Host:  m.c.Conference,
	}.String(),
		"groupchat")
}

func (m *Room) SendXGroupPaused() {
	if m == nil {
		yo.Warn("room is nil")
		return
	}
	m.c.c.SendPaused(xmpp.JID{
		Local: m.Name,
		Host:  m.c.Conference,
	}.String(),
		"groupchat")
}

func (m *Room) SendXPrivateComposing(target string) {
	if m == nil {
		yo.Warn("room is nil")
		return
	}
	m.c.c.SendComposing(xmpp.JID{
		Local: m.Name,
		Host:  m.c.Conference,
		Node:  target,
	}.String(),
		"chat")
}

func (m *Room) SendXPrivatePaused(target string) {
	if m == nil {
		yo.Warn("room is nil")
		return
	}
	m.c.c.SendPaused(xmpp.JID{
		Local: m.Name,
		Host:  m.c.Conference,
		Node:  target,
	}.String(),
		"chat")
}

func (m *Room) GetUsernames() []string {
	return m.Mp.SortedNames()
}

func (m *Room) GroupFingerprint(user string) string {
	return m.Mp.Fingerprint(user)
}
