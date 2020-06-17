package dog

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Cryptodog/go-cryptodog-newprotocol/proto"
	"github.com/gorilla/websocket"

	"github.com/Cryptodog/go-cryptodog/multiparty"
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
	time         time.Time
	rooms        map[string]*Room
	roomsLock    sync.Mutex
	handlers     map[EventType][]EventHandler
	handlersLock sync.Mutex
	killed       bool
	errc         chan error
}

func New() *Conn {
	cn := &Conn{}
	cn.rooms = make(map[string]*Room)
	cn.handlers = make(map[EventType][]EventHandler)
	cn.errc = make(chan error)

	cn.On(UserJoined, cn.introduction)
	cn.On(RoomJoined, cn.introduction)
	cn.On(Disconnected, cn.reconnect)

	return cn
}

func (c *Conn) ActiveRooms() []string {
	ss := []string{}
	c.roomsLock.Lock()
	for k := range c.rooms {
		ss = append(ss, k)
	}
	c.roomsLock.Unlock()
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
		// Use an in-memory store.
		c.DB = new(sync.Map)
	}

	c.time = time.Now()

	c.initKeys()

	go c.connectAllRooms()

	return <-c.errc
}

func (c *Conn) Uptime() time.Duration {
	return time.Since(c.time)
}

func (c *Conn) SetMods(s []string) {
	c.storeJSON("mods", s)
}

func (c *Conn) GetMods() []string {
	var s []string
	c.loadJSON("mods", &s)
	return s
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
	c.roomsLock.Lock()
	for _, r := range c.rooms {
		r.Destroy()
	}

	c.rooms = nil
	c.rooms = make(map[string]*Room)

	for k, v := range c.loadRooms() {
		c.joinRoom(k, v)
	}

	c.roomsLock.Unlock()
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

func (c *Conn) storeRooms() {
	ms := make(map[string]string)
	for k, v := range c.rooms {
		ms[k] = v.Nickname
	}
	c.storeJSON("rooms", ms)
}

func (c *Conn) JoinRoom(room, nick string) {
	if c == nil {
		panic("Cannot join with nil connection")
	}

	c.roomsLock.Lock()
	defer c.roomsLock.Unlock()
	if r := c.rooms[room]; r != nil {
		return
	}

	c.joinRoom(room, nick)
}

func (c *Conn) joinRoom(room, nick string) {
	var err error
	r := new(Room)
	r.ModerationTables = make(map[string][]string)
	r.Name = room
	r.Nickname = nick
	r.Multiparty, err = multiparty.NewMe(nick, c.loadString("mp"))
	if err != nil {
		panic(err)
	}
	r.Multiparty.Out(r.transmitMp)
	r.socket, _, err = websocket.DefaultDialer.Dial(c.URL, nil)
	if err != nil {
		c.errc <- err
		return
	}

	r.Members = make(map[string]*Member)
	c.rooms[room] = r
	c.storeRooms()

	go func() {
		for {
			_, msg, err := r.socket.ReadMessage()
			if err != nil {
				c.emit(Event{
					Type: Disconnected,
					Body: err.Error(),
				})
				r.Destroy()
				return
			}

			if err := r.handleServerMessage(msg); err != nil {
				c.emit(Event{
					Type: Disconnected,
					Body: err.Error(),
				})
				r.Destroy()
				return
			}
		}
	}()

	go func(_room *Room) {
		time.Sleep(200 * time.Millisecond)
		_room.Multiparty.RequestPublicKey("")
		_room.Multiparty.SendPublicKey("")
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

func (c *Conn) Disconnect() error {
	c.roomsLock.Lock()
	defer c.roomsLock.Unlock()
	for _, r := range c.rooms {
		r.Destroy()
	}
	c.errc <- nil
	return nil
}

func (c *Conn) reconnect(ev Event) {
	c.LeaveRoom(ev.Room)
	c.JoinRoom(ev.Room, ev.User)
}

func (c *Conn) LeaveRoom(roomName string) error {
	room := c.GetRoom(roomName)
	if room == nil {
		return fmt.Errorf("dog: no such room as %s", roomName)
	}

	room.writeMessage(&proto.LeaveMessage{})

	room.Destroy()

	delete(c.rooms, roomName)
	c.storeRooms()
	c.roomsLock.Unlock()
	return nil
}
