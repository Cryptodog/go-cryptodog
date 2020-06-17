package dog

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Cryptodog/go-cryptodog-newprotocol/proto"

	"github.com/Cryptodog/go-cryptodog/multiparty"
	"github.com/coyim/otr3"
	"github.com/gorilla/websocket"
	"github.com/superp00t/etc/yo"
)

type Room struct {
	Name             string
	Nickname         string
	Multiparty       *multiparty.Me
	Members          map[string]*Member
	membersLock      sync.Mutex
	ModerationTables map[string][]string

	conn        *Conn
	killed      bool
	socket      *websocket.Conn
	bexAddTout  chan float64
	bexTx       chan []BEX
	joinedEvent bool
}

type Member struct {
	Bot      bool
	r        *Room
	nickname string
	otr      *otr3.Conversation
	onInit   func()
}

func (m *Member) Name() string {
	return m.nickname
}

func (c *Conn) GetRoom(name string) *Room {
	c.roomsLock.Lock()
	room := c.rooms[name]
	c.roomsLock.Unlock()
	return room
}

func (c *Conn) GM(room string, body string) {
	if !utf8.ValidString(body) {
		yo.Warn("invalid utf-8 string!")
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
	} else {
		yo.Warn("Group", room, "doesn't exist!")
	}
}

func (r *Room) GM(body string) {
	r.Group([]byte(body))
}

func (r *Room) Group(b []byte) {
	r.Multiparty.SendMessage(b)
}

func (r *Room) DM(user, data string) {
	r.GetMember(user).DM(data)
}

func (r *Room) IsMod(user string) bool {
	for _, v := range r.conn.GetMods() {
		if v == r.GroupFingerprint(user) {
			return true
		}
	}

	return false
}

func (r *Room) GetMember(s string) *Member {
	if r == nil {
		return nil
	}
	r.membersLock.Lock()
	mem := r.Members[s]
	r.membersLock.Unlock()
	return mem
}

func (m *Member) DM(data string) {
	if m == nil {
		return
	}

	if m.r.conn.opt(DMDisabled) {
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
		m.Raw(string(v))
	}
}

func (m *Member) initOtr() {
	if m.r.conn.opt(DMDisabled) {
		return
	}

	if m.otr == nil {
		key := new(otr3.DSAPrivateKey)
		b64, err := base64.StdEncoding.DecodeString(m.r.conn.loadString("otr"))
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
	r.socket.Close()
}

func (r *Room) emit(e Event) {
	e.Room = r.Name
	r.conn.emit(e)
}

func (r *Room) transmitMp(b []byte) {
	r.writeMessage(&proto.GroupMessage{
		Text: string(b),
	})
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
				m.Raw(string(v))
			}
		}
	}
}

func (m *Member) prepareAnswer(answer string, ask bool) string {
	buddyMpFingerprint := m.r.Multiparty.Fingerprint(m.nickname)

	first := ""
	second := ""
	answer = strings.ToLower(answer)
	for _, v := range []rune(".,'\";?!") {
		answer = strings.Replace(answer, string(v), "", -1)
	}

	me := m.r.Multiparty.Fingerprint("")

	if buddyMpFingerprint != "" {
		if ask {
			first = me
		} else {
			first = buddyMpFingerprint
		}

		if ask {
			second = buddyMpFingerprint
		} else {
			second = me
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
				m.Raw(string(v))
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
				m.Raw(string(v))
			}
		} else {
			yo.Warn(err)
		}
	} else {
		yo.Println(m.nickname, "is already connected")
		doAsk()
	}
}

func (m *Member) Raw(str string) {
	m.r.writeMessage(&proto.PrivateMessage{
		To:   m.Name(),
		Text: str,
	})
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

func (m *Room) GetUsernames() []string {
	return m.Multiparty.SortedNames()
}

func (m *Room) GroupFingerprint(user string) string {
	return m.Multiparty.Fingerprint(user)
}

func (m *Room) writeMessage(msg proto.SpecificMessage) error {
	packedMsg := msg.Pack()
	return m.socket.WriteMessage(websocket.TextMessage, []byte(packedMsg.String()))
}

func (m *Room) handleUserQuit(lm *proto.LeaveMessage) {
	m.membersLock.Lock()
	delete(m.Members, lm.Name)
	m.membersLock.Unlock()
	m.Multiparty.DestroyUser(lm.Name)
	m.emit(Event{
		Type: UserLeft,
		Room: m.Name,
		User: lm.Name,
	})
}

func (m *Room) handleUserJoin(jm *proto.JoinMessage) {

}

func (m *Room) handleGroupMessage(gm *proto.GroupMessage) {
	if m.IsBlocked(gm.From) {
		return
	}

	newUser, data, err := m.Multiparty.ReceiveMessage(gm.From, gm.Text)
	if err != nil {
		yo.Warn(err)
		return
	}

	if newUser != "" {
		m.membersLock.Lock()
		m.Members[gm.From] = &Member{
			false,
			m,
			newUser,
			nil,
			nil,
		}
		go func() {
			m.emit(Event{
				Type: UserJoined,
				User: newUser,
				Room: m.Name,
			})
		}()
	}

	if len(data) > 0 {
		m.conn.processGroupchatBytes(m.Name, gm.From, data)
	}
}

func (m *Room) handlePrivateMessage(pm *proto.PrivateMessage) {
	if m.conn.opt(DMDisabled) {
		yo.L(4).Warn("DMs are disabled")
		return
	}

	memb := m.GetMember(pm.From)
	if memb == nil {
		yo.Warn("No member", pm.From)
		return
	}

	memb.initOtr()

	plain, toSend, err := memb.otr.Receive(otr3.ValidMessage(pm.Text))
	if err != nil {
		yo.L(4).Warn(err)
	} else {
		for _, v := range toSend {
			memb.Raw(string(v))
		}
		if str := string(plain); str != "" {
			m.conn.processPrivateString(m.Name, pm.From, str)
		}
	}
}

func (m *Room) handleRosterMessage(rm *proto.RosterMessage) {

}

func (m *Room) handleServerMessage(data []byte) error {
	var mType proto.Type
	if len(data) == 0 {
		return fmt.Errorf("dog: empty packet received from server")
	}

	mType = proto.Type(data[0])
	rawMessage := data[1:]
	var err error

	switch mType {
	case proto.TypeJoinMessage:
		decoded := proto.JoinMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err == nil {
			m.handleUserJoin(&decoded)
		}
	case proto.TypeLeaveMessage:
		decoded := proto.LeaveMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err == nil {
			m.handleUserQuit(&decoded)
		}
	case proto.TypeGroupMessage:
		decoded := proto.GroupMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err == nil {
			m.handleGroupMessage(&decoded)
		}
	case proto.TypePrivateMessage:
		decoded := proto.PrivateMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err != nil {
			m.handlePrivateMessage(&decoded)
		}
	case proto.TypeRosterMessage:
		decoded := proto.RosterMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err != nil {
			m.handleRosterMessage(&decoded)
		}
	case proto.TypeErrorMessage:
		decoded := proto.ErrorMessage{}
		err = json.Unmarshal(rawMessage, &decoded)
		if err == nil {
			switch decoded.Error {
			case "Nickname in use.":
				m.emit(Event{
					Type: NicknameInUse,
				})
				return nil
			default:
				return fmt.Errorf("server returned error: %s", decoded.Error)
			}
		}
	default:
		err = fmt.Errorf("Unknown message type: %s", mType)
	}

	return err
}
