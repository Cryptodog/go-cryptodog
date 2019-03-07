package dog

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"sync"
	"time"

	"github.com/Cryptodog/go-cryptodog/multiparty"
	"github.com/Cryptodog/go-cryptodog/xmpp"
	"github.com/coyim/otr3"
	"github.com/superp00t/etc/yo"
)

const (
	// Opts flags
	BEXDisabled uint64 = 1 << 0
	DMDisabled  uint64 = 1 << 1
	DebugXMPP   uint64 = 1 << 2
	Human       uint64 = 1 << 3
)

type Database interface {
	Load(k interface{}) (interface{}, bool)
	Store(k, v interface{})
	Delete(k interface{})
}

type Conn struct {
	// Public Options
	DB         Database
	URL        string
	Host       string
	Conference string
	Proxy      string
	Opts       uint64

	// Internal variables
	time   time.Time
	c      *xmpp.Conn
	rooms  map[string]*Room
	rl     *sync.Mutex
	h      map[EventType][]EventHandler
	hl     *sync.Mutex
	killed bool
	errc   chan error
}

func New() *Conn {
	cn := &Conn{}
	cn.rooms = make(map[string]*Room)
	cn.h = make(map[EventType][]EventHandler)
	cn.errc = make(chan error)
	cn.rl = new(sync.Mutex)
	cn.hl = new(sync.Mutex)

	cn.On(UserJoined, cn.introduction)
	cn.On(RoomJoined, cn.introduction)

	return cn
}

func (c *Conn) ActiveRooms() []string {
	ss := []string{}
	c.rl.Lock()
	for k := range c.rooms {
		ss = append(ss, k)
	}
	c.rl.Unlock()
	return ss
}

func (c *Conn) opt(v uint64) bool {
	return c.Opts&v != 0
}

func (c *Conn) Run() error {
	if c.URL == "" {
		c.URL = "wss://crypto.dog/websocket"
	}

	if c.Host == "" {
		c.Host = "crypto.dog"
	}

	if c.Conference == "" {
		c.Conference = "conference.crypto.dog"
	}

	if c.DB == nil {
		c.DB = new(sync.Map)
	}

	c.time = time.Now()

	c.initKeys()

	go c.populateConnection()

	return <-c.errc
}

func (c *Conn) populateConnection() {
	var errPeriod = time.Duration(2 * time.Second)

	period := errPeriod

	for {
		var err error

		c.c, err = xmpp.Dial(xmpp.Opts{
			URL:   c.URL,
			Host:  c.Host,
			Proxy: c.Proxy,
			Debug: c.opt(DebugXMPP),
		})
		if err != nil {
			goto hndlErr
		}

		period = errPeriod

		c.connectAllRooms()

		c.emit(Event{
			Type: Connected,
		})

		for {
			err = c.processEvent()
			if err != nil {
				goto hndlErr
				return
			}
		}

	hndlErr:
		c.c.Disconnect()

		c.emit(Event{
			Type: Disconnected,
		})

		if c.killed == true {
			return
		}

		yo.L(4).Warn(err)
		period += time.Duration(
			float64(period) * 1.6,
		)
		yo.L(4).Warn("waiting", period, "to reconnect")
		time.Sleep(period)
		continue
	}
}

func (c *Conn) processEvent() error {
	i, err := c.c.Recv()
	if err != nil {
		switch err {
		case xmpp.RateLimited:
			c.emit(Event{
				Type: RateLimit,
			})
			return nil
		default:
			return err
		}
	}

	switch m := i.(type) {
	case xmpp.Message:
		go c.processMessage(m)
	case xmpp.NicknameInUse:
		c.emit(Event{
			Type: NicknameInUse,
			Room: m.Local,
			User: m.Node,
		})
	case xmpp.Presence:
		jid, err := xmpp.ParseJID(m.From)
		if err != nil {
			return err
		}

		nick := jid.Node

		rm := c.GetRoom(jid.Local)
		if rm != nil {
			if jid.Host == c.Conference {
				if jid.Node == rm.MyName {
					if rm.joinedEvent == false {
						rm.joinedEvent = true
						go func() {
							time.Sleep(2000 * time.Millisecond)
							rm.emit(Event{
								Type: RoomJoined,
							})
						}()
					}
				}
			}
		}

		switch m.Type {
		case "unavailable":
			go func() {
				rm := c.GetRoom(jid.Local)
				rm.ml.Lock()
				delete(rm.Members, nick)
				rm.ml.Unlock()
				rm.Mp.DestroyUser(nick)
				c.emit(Event{
					Type: UserLeft,
					Room: jid.Local,
					User: nick,
				})
			}()
		}
	}

	return nil
}

func (c *Conn) processMessage(msg xmpp.Message) {
	jid, err := xmpp.ParseJID(msg.From)
	if err != nil {
		yo.Warn(err)
		return
	}
	nick := jid.Node
	to, err := xmpp.ParseJID(msg.To)
	if err != nil {
		yo.Warn(err)
		return
	}
	switch msg.Type {
	case "groupchat":
		rm := c.GetRoom(jid.Local)
		if rm == nil {
			yo.Fatal(to.Local, "does not exist", msg.To)
		}

		if nick == rm.MyName {
			return
		}

		if msg.Id == "composing" {
			rm.emit(Event{
				Type: Composing,
				User: nick,
			})
			return
		}

		if msg.Id == "paused" {
			rm.emit(Event{
				Type: Paused,
				User: nick,
			})
			return
		}

		newUser, data, err := rm.Mp.ReceiveMessage(nick, msg.Body)
		if err != nil {
			yo.L(4).Warn(err)
		} else {
			if newUser != "" {
				rm.ml.Lock()
				rm.Members[nick] = &Member{
					rm,
					nick,
					nil,
					nil,
				}
				rm.ml.Unlock()
				if time.Since(c.time) > 4000*time.Millisecond {
					go func() {
						time.Sleep(200 * time.Millisecond)
						c.emit(Event{
							Type: UserJoined,
							User: nick,
							Room: jid.Local,
						})
					}()
				}
			}

			if len(data) > 0 {
				c.processGroupchatBytes(jid.Local, nick, data)
			}
		}
	case "chat":
		if c.opt(DMDisabled) {
			yo.L(4).Warn("DMs are disabled")
			return
		}

		yo.L(4).Warn("DM not disabled")

		room := c.GetRoom(jid.Local)
		memb := room.GetMember(nick)
		if memb == nil {
			yo.Warn("No member", nick)
			return
		}

		yo.L(4).Warn("member exists", nick)

		memb.initOtr()
		plain, toSend, err := memb.otr.Receive(otr3.ValidMessage(msg.Body))
		if err != nil {
			yo.Warn(msg.Body)
			yo.L(4).Warn(err)
		} else {
			targetJID := xmpp.JID{
				Local: jid.Local,
				Host:  c.Conference,
				Node:  nick,
			}
			yo.L(4).Warn("Sending off", len(toSend), targetJID)
			for _, v := range toSend {
				c.c.SendMessage(targetJID.String(), "chat", string(v))
			}
			if str := string(plain); str != "" {
				c.processPrivateString(jid.Local, nick, str)
			}
		}
	}
}

func (c *Conn) processGroupchatBytes(room, user string, body []byte) {
	if len(body) > 3 && bytes.Equal(body[:3], BEX_MAGIC) {
		if !c.opt(BEXDisabled) {
			c.GetRoom(room).handleGroupBEXPacket(user, body)
		}
	} else {
		c.emit(Event{
			Type: GroupMessage,
			Room: room,
			User: user,
			Body: string(body),
		})
	}
}

func (c *Conn) processPrivateString(room, user string, body string) {
	b64, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		c.emit(Event{
			Type:    PrivateMessage,
			Private: true,
			Room:    room,
			User:    user,
			Body:    body,
		})
	} else {
		if len(b64) > 3 && bytes.Equal(b64[:3], BEX_MAGIC) {
			if !c.opt(BEXDisabled) {
				c.GetRoom(room).handlePrivateBEXPacket(user, b64)
			}
		} else {
			c.emit(Event{
				Type:    PrivateMessage,
				Private: true,
				Room:    room,
				User:    user,
				Body:    body,
			})
		}
	}
}

func (c *Conn) loadRooms() map[string]string {
	var rooms map[string]string
	c.loadJSON("rooms", &rooms)
	return rooms
}

func (c *Conn) loadJSON(str string, v interface{}) {
	data, ok := c.DB.Load(str)
	if ok {
		json.Unmarshal([]byte(data.(string)), v)
	}
}

func (c *Conn) loadString(str string) string {
	data, ok := c.DB.Load(str)
	if ok {
		return data.(string)
	}

	return ""
}

func (c *Conn) storeString(key, value string) {
	c.DB.Store(key, value)
}

func (c *Conn) storeJSON(str string, v interface{}) {
	dat, _ := json.MarshalIndent(v, "", "  ")
	c.DB.Store(str, string(dat))
}

func (c *Conn) connectAllRooms() {
	c.rl.Lock()
	for _, r := range c.rooms {
		r.Destroy()
	}

	c.rooms = nil
	c.rooms = make(map[string]*Room)

	for k, v := range c.loadRooms() {
		c.joinMuc(k, v)
	}

	c.rl.Unlock()
}

func (c *Conn) initKeys() {
	if c.loadString("mp") == "" {
		buf := make([]byte, 32)
		rand.Read(buf)
		c.storeString("mp", base64.StdEncoding.EncodeToString(buf))
	}

	if c.loadString("otr") == "" && !c.opt(DMDisabled) {
		ok := new(otr3.DSAPrivateKey)
		ok.Generate(rand.Reader)

		serial := base64.StdEncoding.EncodeToString(ok.Serialize())

		c.storeString("otr", serial)

		yo.L(4).Ok("OTR KEY", serial)
	}
}

func (c *Conn) JoinRoom(room, nick string) {
	if c == nil {
		yo.L(4).Warn("Cannot join with nil connection")
		return
	}

	c.rl.Lock()
	defer c.rl.Unlock()
	if c.rooms[room] != nil {
		return
	}

	c.joinMuc(room, nick)
}

func (c *Conn) joinMuc(room, nick string) {
	r := new(Room)
	r.Name = room
	r.MyName = nick
	r.Mp, _ = multiparty.NewMe(nick, c.loadString("mp"))
	r.Mp.Out(r.transmitMp)
	r.c = c
	r.Members = make(map[string]*Member)
	r.ml = new(sync.Mutex)
	c.rooms[room] = r
	ms := make(map[string]string)
	for k, v := range c.rooms {
		ms[k] = v.MyName
	}
	c.storeJSON("rooms", ms)

	r.bexTx = make(chan []BEX)
	r.bexAddTout = make(chan float64)
	go r.bexGroupTransmitter()

	c.c.JoinMUC(r.Name, c.Conference, r.MyName)

	go func(_room *Room) {
		time.Sleep(200 * time.Millisecond)
		_room.Mp.RequestPublicKey("")
		_room.Mp.SendPublicKey("")
	}(r)
}

func (c *Conn) DM(room, user, message string) {
	c.GetRoom(room).DM(user, message)
}

func (c *Conn) Answer(room, user, answer string) {
	c.GetRoom(room).GetMember(user).Answer(answer)
}

func (c *Conn) Ask(room, user, question, answer string) {
	c.GetRoom(room).GetMember(user).Ask(question, answer)
}

func (c *Conn) Disconnect() {
	if c == nil {
		return
	}

	c.killed = true
	go func() {
		c.errc <- nil
	}()

	c.c.Disconnect()
}
