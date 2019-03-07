package dog

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/superp00t/etc"
	"github.com/superp00t/etc/yo"

	"encoding/hex"

	"encoding/base64"
	"encoding/json"

	"golang.org/x/crypto/nacl/secretbox"
)

var (
	// Separates this extension from a regular plaintext Cryptodog message.
	BEX_MAGIC = []byte{0x04, 0x45, 0xFF}
)

type BEXHeader uint64

const (
	NOT_VALID BEXHeader = 0
	// Packet headers
	SET_COLOR            BEXHeader = 1
	PING                 BEXHeader = 2
	PONG                 BEXHeader = 3
	BEX_COMPOSING        BEXHeader = 4
	BEX_PAUSED           BEXHeader = 5
	FILE_ATTACHMENT      BEXHeader = 6
	TEXT_MESSAGE         BEXHeader = 7
	FLAG_ME_AS_BOT       BEXHeader = 8
	STATUS_ONLINE        BEXHeader = 9
	STATUS_AWAY          BEXHeader = 10
	MOD_ELECTED          BEXHeader = 11
	REMOVE_DEAD_USERS    BEXHeader = 12
	SET_MODERATION_TABLE BEXHeader = 13
	SET_LOCKDOWN_LEVEL   BEXHeader = 14
	WHITELIST_USER       BEXHeader = 15

	// WebRTC
	ICE_CANDIDATE         BEXHeader = 30
	RTC_OFFER             BEXHeader = 31
	RTC_ANSWER            BEXHeader = 32
	RTC_SIGNAL_CAPABILITY BEXHeader = 33
	RTC_SIGNAL_DISABLED   BEXHeader = 34
)

type BEX struct {
	Header               BEXHeader
	Color                string
	Status               string
	File                 *File
	MessageType, Message string
	SDPType              string
	SDPData              string
	ICECandidate         string
	SDPMLineIndex        uint64
	SDPMid               string
	Target               string
	Level                uint64
	TableKey             string
	Table                []string
}

func (b BEXHeader) String() string {
	switch b {
	case SET_COLOR:
		return "change color"
	case STATUS_ONLINE:
		return "came online"
	case STATUS_AWAY:
		return "went offline"
	case BEX_COMPOSING:
		return "typing..."
	case BEX_PAUSED:
		return "stopped typing"
	case FILE_ATTACHMENT:
		return "file attachment"
	case TEXT_MESSAGE:
		return "utf8 bex string"
	case FLAG_ME_AS_BOT:
		return "I am a bot"
	case ICE_CANDIDATE:
		return "got ICE candidate"
	case RTC_OFFER:
		return "got WebRTC offer"
	case RTC_ANSWER:
		return "got WebRTC answer"
	case MOD_ELECTED:
		return "mod subscription"
	}

	return fmt.Sprintf("unknown BEX (%d)", b)
}

type File struct {
	PrefixSize uint64
	FileKey    *[32]byte
	FileNonce  *[24]byte
	FileMIME   string
	FileUUID   etc.UUID
}

func ContainsBEXHeader(input []byte) bool {
	if len(input) < 3 {
		return false
	}

	return bytes.Equal(input[:3], BEX_MAGIC)
}

func DecodeBEX(input []byte) ([]BEX, error) {
	e := etc.FromBytes(input)
	var b []BEX

	header := e.ReadBytes(3)
	if !ContainsBEXHeader(header) {
		return nil, fmt.Errorf("phoxy: not a BEX message")
	}

	length := e.ReadUint()

	if length > 8 {
		return nil, fmt.Errorf("phoxy: too many BEX submessages")
	}

	for x := uint64(0); x < length; x++ {
		bx := BEX{
			Header: BEXHeader(e.ReadUint()),
		}

		switch bx.Header {
		case BEX_COMPOSING, BEX_PAUSED, FLAG_ME_AS_BOT, STATUS_AWAY, STATUS_ONLINE, REMOVE_DEAD_USERS:
			// no body
		case SET_COLOR:
			bx.Color = fmt.Sprintf("#%X%X%X", e.ReadByte(), e.ReadByte(), e.ReadByte())
		case FILE_ATTACHMENT:
			bx.File = new(File)
			bx.File.PrefixSize = e.ReadUint()
			bx.File.FileKey = e.ReadBoxKey()
			bx.File.FileNonce = e.ReadBoxNonce()
			bx.File.FileMIME = e.ReadUString()
			bx.File.FileUUID = e.ReadUUID()
		case TEXT_MESSAGE:
			bx.MessageType = e.ReadUString()
			bx.Message = e.ReadUString()
		case RTC_OFFER, RTC_ANSWER:
			bx.Target = e.ReadUString()
			bx.SDPData = e.ReadUString()
		case ICE_CANDIDATE:
			bx.Target = e.ReadUString()
			bx.ICECandidate = e.ReadUString()
			bx.SDPMLineIndex = e.ReadUint()
			bx.SDPMid = e.ReadUString()
		case WHITELIST_USER:
			bx.Target = e.ReadUString()
		case SET_LOCKDOWN_LEVEL:
			bx.Level = e.ReadUint()
		case SET_MODERATION_TABLE:
			bx.TableKey = e.ReadUString()
			ln := e.ReadUint()
			if ln > 512 {
				break
			}
			bx.Table = make([]string, int(ln))
			for x := range bx.Table {
				bx.Table[x] = e.ReadUString()
			}
		case MOD_ELECTED:
			bx.Target = e.ReadUString()
		default:
			yo.L(4).Warn("received unknown bex type", bx.Header)
			break
		}

		b = append(b, bx)
	}

	return b, nil
}

func EncodeBEX(b []BEX) []byte {
	e := etc.NewBuffer()
	e.Write(BEX_MAGIC)
	e.WriteUint(uint64(len(b)))

	for _, bx := range b {
		e.WriteUint(uint64(bx.Header))

		switch bx.Header {
		case BEX_COMPOSING, BEX_PAUSED, FLAG_ME_AS_BOT, STATUS_AWAY, STATUS_ONLINE:
			// no body
		case SET_COLOR:
			if ValidColor(bx.Color) == true {
				hx, _ := hex.DecodeString(bx.Color[1:])
				// hx ==  { 0xAA, 0xBB, 0xCC }
				e.Write(hx[:3])
			} else {
				e.Write([]byte{0, 0, 0})
			}
		case FILE_ATTACHMENT:
			e.WriteUint(bx.File.PrefixSize)
			e.Write(bx.File.FileKey[:])
			e.Write(bx.File.FileNonce[:])
			e.WriteUString(bx.File.FileMIME)
			e.WriteUUID(bx.File.FileUUID)
		case TEXT_MESSAGE:
			e.WriteUString(bx.MessageType)
			e.WriteUString(bx.Message)
		case RTC_OFFER, RTC_ANSWER:
			e.WriteUString(bx.Target)
			e.WriteUString(bx.SDPData)
		case ICE_CANDIDATE:
			e.WriteUString(bx.Target)
			e.WriteUString(bx.ICECandidate)
			e.WriteUint(bx.SDPMLineIndex)
			e.WriteUString(bx.SDPMid)
		case SET_LOCKDOWN_LEVEL:
			e.WriteUint(bx.Level)
		case SET_MODERATION_TABLE:
			e.WriteUString(bx.TableKey)
			e.WriteUint(uint64(len(bx.Table)))
			for _, v := range bx.Table {
				e.WriteUString(v)
			}
		case MOD_ELECTED:
			e.WriteUString(bx.Target)
		case WHITELIST_USER:
			e.WriteUString(bx.Target)
		}
	}

	return e.Bytes()
}

type ICECandidate struct {
	Data          string `json:"data"`
	SDPMid        string `json:"sdpMid"`
	SDPMLineIndex uint64 `json:"sdpMLineIndex"`
}

func (r *Room) handleGroupBEXPacket(from string, data []byte) {
	b, err := DecodeBEX(data)
	if err != nil {
		yo.L(4).Warn(err)
		return
	}

	for _, bx := range b {
		switch bx.Header {
		case FILE_ATTACHMENT:
			r.emit(Event{
				Type: FileAttachment,
				User: from,
				File: bx.File,
			})
		case BEX_COMPOSING:
			r.emit(Event{
				Type: Composing,
				User: from,
			})
		case BEX_PAUSED:
			r.emit(Event{
				Type: Paused,
				User: from,
			})
		case SET_COLOR:
			r.emit(Event{
				Type: ColorModify,
				User: from,
				Body: bx.Color,
			})
		case RTC_ANSWER:
			if bx.Target == r.MyName {
				r.emit(Event{
					Type: WebRTCAnswer,
					User: from,
					Body: bx.SDPData,
				})
			}
		case RTC_OFFER:
			if bx.Target == r.MyName {
				r.emit(Event{
					Type: WebRTCOffer,
					User: from,
					Body: bx.SDPData,
				})
			}
		case ICE_CANDIDATE:
			if bx.Target == r.MyName {
				js, _ := json.Marshal(ICECandidate{
					bx.ICECandidate,
					bx.SDPMid,
					bx.SDPMLineIndex,
				})

				r.emit(Event{
					Type: WebRTCIceCandidate,
					User: from,
					Body: string(js),
				})
			}
		case RTC_SIGNAL_CAPABILITY:
			r.emit(Event{
				Type: WebRTCCapable,
				User: from,
			})
		case MOD_ELECTED:
			r.emit(Event{
				Type: SubscribedToModerator,
				User: from,
				Body: bx.Target,
			})
		}
	}
}

func (r *Room) handlePrivateBEXPacket(from string, data []byte) {
	b, err := DecodeBEX(data)
	if err != nil {
		yo.L(4).Warn(err)
		return
	}

	for _, bx := range b {
		switch bx.Header {
		case FILE_ATTACHMENT:
			r.emit(Event{
				User:    from,
				Private: true,
				File:    bx.File,
			})
		case BEX_COMPOSING:
			r.emit(Event{
				Type:    Composing,
				Private: true,
				User:    from,
			})
		case BEX_PAUSED:
			r.emit(Event{
				Type:    Paused,
				User:    from,
				Private: true,
			})
		}
	}
}

func (r *Room) bexGroupTransmitter() {
	var tmp []BEX
	for {
		if r.killed {
			return
		}

		var tout = float64(100)

		select {
		case <-time.After(time.Duration(tout) * time.Millisecond):
			if len(tmp) == 0 {
				if tout > 0 {
					// AIMD
					tout *= .75
				}
				continue
			}
			if len(tmp) > 2 {
				for x := 0; x < len(tmp)/2; x++ {
					low := x * 2
					high := (x + 1) * 2

					if high > len(tmp) {
						high = len(tmp)
					}

					yo.Ok("Sending range ", low, "-", "high")

					slice := EncodeBEX(tmp[low:high])
					r.Group(slice)

					z := time.Millisecond * time.Duration(float64(len(slice))*1.2)

					yo.Okf("Sleeping for %+v (%d/%d)", z, x, (len(tmp) / 3))
					time.Sleep(z)
				}

				tmp = []BEX{}
			} else {
				d := EncodeBEX(tmp)
				r.Group(d)
				tmp = []BEX{}
			}
			if tout < 2000 {
				tout += 900
			}
		case t := <-r.bexAddTout:
			tout += t
		case t := <-r.bexTx:
			tmp = append(tmp, t...)
		}
	}
}

func (r *Room) SendBEXGroup(b []BEX) {
	go func(b []BEX) {
		r.bexTx <- b
	}(b)
}

func (r *Room) SendBEXPrivate(nickname string, b []BEX) {
	data := EncodeBEX(b)

	r.DM(nickname, base64.StdEncoding.EncodeToString(data))
}

func (r *Room) Download(f *File) ([]byte, error) {
	h, err := http.Get(BexServer + "/files/" + f.FileUUID.String())
	if err != nil {
		return nil, err
	}

	switch h.StatusCode {
	case http.StatusNotFound:
		return nil, fmt.Errorf("file was deleted")
	case http.StatusBadGateway:
		return nil, fmt.Errorf("server is down")
	}

	byts, err := ioutil.ReadAll(h.Body)
	if err != nil {
		return nil, err
	}

	dat, ok := secretbox.Open(nil, byts, f.FileNonce, f.FileKey)
	if !ok {
		return nil, fmt.Errorf("could not decrypt")
	}

	if len(dat) <= int(f.PrefixSize) {
		return nil, fmt.Errorf("invalid prefix size")
	}

	return dat[int(f.PrefixSize):], nil
}

const BexServer = "https://bex.pg.ikrypto.club"

func (r *Room) Upload(fileMime string, data []byte) (*File, error) {
	fl := new(File)
	fl.PrefixSize = etc.RandomBigInt(big.NewInt(4000), big.NewInt(14000)).Uint64()
	fl.FileNonce = new([24]byte)
	fl.FileKey = new([32]byte)
	io.ReadFull(rand.Reader, fl.FileNonce[:])
	io.ReadFull(rand.Reader, fl.FileKey[:])
	fl.FileMIME = fileMime

	dbuf := etc.NewBuffer()
	dbuf.WriteRandom(int(fl.PrefixSize))
	dbuf.Write(data)

	ciph := secretbox.Seal(nil, dbuf.Bytes(), fl.FileNonce, fl.FileKey)
	box := etc.FromBytes(ciph)

	resp, err := http.Post(
		fmt.Sprintf("%s/upload?cl=%d", BexServer, len(ciph)),
		"application/octet-stream",
		box)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("too many requests")
	}

	b, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	e := etc.FromBytes(b)
	fl.FileUUID = e.ReadUUID()

	return fl, nil
}

func ValidColor(str string) bool {
	mtch, _ := regexp.MatchString("^#[a-fA-F0-9]{6}$", str)
	return mtch
}

func (c *Conn) introduction(event Event) {
	if c.opt(BEXDisabled) == false {
		var intro []BEX
		var color string
		if color = c.loadString("color"); color == "" {
			color = "#413ed1"
			c.storeString("color", color)
		}

		intro = append(intro, BEX{
			Header: SET_COLOR,
			Color:  color,
		})

		if c.opt(Human) == false {
			intro = append(intro, BEX{
				Header: FLAG_ME_AS_BOT,
			})
		}

		c.GetRoom(event.Room).SendBEXGroup(intro)
	}
}

func (c *Conn) SetColor(color string) error {
	if !ValidColor(color) {
		return fmt.Errorf("dog: invalid color: '%s'", color)
	}

	c.storeString("color", color)

	if c.opt(BEXDisabled) == true {
		return nil
	}

	packet := []BEX{
		{Header: SET_COLOR, Color: color},
	}

	for _, v := range c.ActiveRooms() {
		c.GetRoom(v).SendBEXGroup(packet)
	}

	return nil
}
