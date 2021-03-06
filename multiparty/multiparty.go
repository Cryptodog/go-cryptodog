//Package multiparty implements the Cryptodog Multiparty Protocol as used in Cryptodog version 2.5.0, and previously, Cryptocat 2.
package multiparty

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/curve25519"
)

type KeyExMessage struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type Answer struct {
	Type string                 `json:"type"`
	Text map[string]*TextAnswer `json:"text"`
	Tag  string                 `json:"tag,omitempty"`
}

type TextAnswer struct {
	Message string `json:"message"`
	IV      string `json:"iv,omitempty"`
	Tag     string `json:"tag,omitempty"`
	HMAC    string `json:"hmac,omitempty"`
}

type MPStorage struct {
	Message []byte
	HMAC    []byte
}

type Buddy struct {
	CryptoEnabled bool
	PublicKey     [32]byte
	MpSecretKey   *MPStorage
	HMAC          string
}

type Me struct {
	MaximumMessageSize int
	Name               string
	UsedIVs            []string
	SecretKey          [32]byte
	PublicKey          [32]byte
	SentKey            bool
	Buddies            map[string]*Buddy
	_sendFunc          func([]byte)
	lastBroadcast      time.Time
	buddyLock          sync.Mutex
	keyLock            sync.Mutex
	keyMap             map[string]*time.Time
	blacklist          map[string]bool
}

func (m *Me) lock() {
	m.buddyLock.Lock()
}

func (me *Me) unlock() {
	me.buddyLock.Unlock()
}

func Sha512(input []byte) []byte {
	hasher := sha512.New()
	hasher.Write(input)
	return hasher.Sum(nil)
}

func IsElem(element string, array []string) bool {
	for _, v := range array {
		if v == element {
			return true
		}
	}

	return false
}

func MessageTag(message []byte) string {
	for i := 0; i < 8; i++ {
		message = Sha512(message)
	}

	return base64.StdEncoding.EncodeToString(message)
}

func (me *Me) GenerateKeys() {
	io.ReadFull(rand.Reader, me.SecretKey[:])
	curve25519.ScalarBaseMult(&me.PublicKey, &me.SecretKey)
}

func (me *Me) BlacklistUser(nick string) {
	me.keyLock.Lock()
	me.blacklist[nick] = true
	me.keyLock.Unlock()
}

func (me *Me) UnblacklistUser(nick string) {
	me.keyLock.Lock()
	me.blacklist[nick] = false
	me.keyLock.Unlock()
}

func (me *Me) SendPublicKey(nick string) {
	a := KeyExMessage{
		Type: "public_key",
		Text: base64.StdEncoding.EncodeToString(me.PublicKey[:]),
	}

	str, _ := json.Marshal(a)
	me._sendFunc(str)
}

func HMAC(msg, key []byte) string {
	mac := hmac.New(sha512.New, key)
	mac.Write(msg)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (me *Me) sendMessage(message []byte) {
	buf := make([]byte, 64)
	rand.Read(buf)
	message = append(message, buf...)

	encrypted := Answer{
		Type: "message",
		Text: make(map[string]*TextAnswer),
	}

	var sortedRecipients []string

	me.keyLock.Lock()
	for k, v := range me.Buddies {
		if me.blacklist[k] {
			continue
		}
		if v.CryptoEnabled {
			sortedRecipients = append(sortedRecipients, k)
		}
	}
	me.keyLock.Unlock()

	sort.Strings(sortedRecipients)

	var bhmac []byte

	for _, v := range sortedRecipients {
		if me.Buddies[v] == nil || me.Buddies[v].MpSecretKey == nil {
			continue
		}
		iv := newIV()
		if IsElem(iv, me.UsedIVs) {
			iv = newIV()
		}

		me.UsedIVs = append(me.UsedIVs, iv)

		if encrypted.Text[v] == nil {
			encrypted.Text[v] = &TextAnswer{}
		}
		encrypted.Text[v].Message = encryptAES(message, me.Buddies[v].MpSecretKey.Message, fixIV(iv))
		encrypted.Text[v].IV = iv

		// Append to HMAC
		msge, _ := base64.StdEncoding.DecodeString(encrypted.Text[v].Message)
		bhmac = append(bhmac, msge...)
		ivee, _ := base64.StdEncoding.DecodeString(encrypted.Text[v].IV)
		bhmac = append(bhmac, ivee...)
	}

	tag := message
	for _, ve := range sortedRecipients {
		encrypted.Text[ve].HMAC = HMAC(bhmac, me.Buddies[ve].MpSecretKey.HMAC)

		msge, _ := base64.StdEncoding.DecodeString(encrypted.Text[ve].HMAC)
		tag = append(tag, msge...)
	}

	encrypted.Tag = MessageTag(tag)
	str, _ := json.Marshal(encrypted)

	me._sendFunc(str)
}

func (me *Me) RequestPublicKey(s string) {
	d, _ := json.Marshal(map[string]interface{}{
		"type": "public_key_request",
		"text": s,
	})
	me._sendFunc(d)
}

func (me *Me) Out(f func([]byte)) {
	me._sendFunc = f
}

func (me *Me) receiveMessage(sender string, messageSrc string, mt map[string]interface{}) (string, []byte, error) {
	if sender == me.Name {
		return "", nil, nil
	}

	msg := mt["text"]
	if msg == nil {
		msg = ""
	}

	switch mt["type"] {
	case "public_key":
		str, ok := msg.(string)
		if !ok {
			return "", nil, fmt.Errorf("public key field is not a string")
		}

		if msg == "" {
			return "", nil, fmt.Errorf("message empty")
		}

		publicKey, err := base64.StdEncoding.DecodeString(str)
		if err != nil {
			return "", nil, err
		}

		// Delete their key when they log out. (NYI)
		if me.Buddies[sender] != nil {
			if me.Buddies[sender].CryptoEnabled {
				pk := me.Buddies[sender].PublicKey[:]
				if !bytes.Equal(pk, publicKey) {
					return "", nil, fmt.Errorf("invalid key change")
				} else {
					return "", nil, nil
				}
			}
		}

		var pk [32]byte
		copy(pk[:], publicKey)

		if me.Buddies[sender] == nil {
			me.Buddies[sender] = &Buddy{}
		}

		me.Buddies[sender].CryptoEnabled = true
		me.Buddies[sender].PublicKey = pk

		if me.Buddies[sender].MpSecretKey == nil {
			me.Buddies[sender].MpSecretKey = me.genSharedSecret(sender)
		}

		return sender, nil, nil
	case "public_key_request":
		str, ok := msg.(string)
		if !ok {
			return "", nil, fmt.Errorf("nickname field is not a string")
		}

		if str == me.Name || str == "" {
			me.SendPublicKey(sender)
		}
		return "", nil, nil
	case "message":
		var m Answer
		err := json.Unmarshal([]byte(messageSrc), &m)
		if err != nil {
			return "", nil, err
		}

		if m.Text[me.Name] == nil {
			return "", nil, fmt.Errorf("could not decrypt")
		}

		if me.Buddies[sender] == nil {
			return "", nil, fmt.Errorf("Sender not in buddies")
		}
		var missingrecipients []string
		for r := range me.Buddies {
			if m.Text[r] == nil {
				missingrecipients = append(missingrecipients, r)
				continue
			} else {
				if m.Text[r].Message == "" || m.Text[r].HMAC == "" || m.Text[r].IV == "" {
					missingrecipients = append(missingrecipients, r)
				}
			}
		}

		var sortedRecipients []string

		for k := range m.Text {
			sortedRecipients = append(sortedRecipients, k)
		}

		sort.Strings(sortedRecipients)

		var bhmac []byte

		for _, v := range sortedRecipients {
			if !IsElem(v, missingrecipients) {
				mby, err := base64.StdEncoding.DecodeString(m.Text[v].Message)
				if err != nil {
					return "", nil, err
				}
				bhmac = append(bhmac, mby...)
				ivby, err := base64.StdEncoding.DecodeString(m.Text[v].IV)
				if err != nil {
					return "", nil, err
				}
				bhmac = append(bhmac, ivby...)
			}
		}

		shmac := me.Buddies[sender].MpSecretKey.HMAC
		ddmac := HMAC(bhmac, shmac)
		if m.Text[me.Name].HMAC != ddmac {
			return "", nil, fmt.Errorf("hmac failure")
		}

		if IsElem(m.Text[me.Name].IV, me.UsedIVs) {
			return "", nil, fmt.Errorf("IV reuse detected, possible replay attack")
		}

		me.UsedIVs = append(me.UsedIVs, m.Text[me.Name].IV)

		iv := fixIV(m.Text[me.Name].IV)
		plaintext := decryptAES(m.Text[me.Name].Message, me.Buddies[sender].MpSecretKey.Message, iv)
		mtag := plaintext
		for _, v := range sortedRecipients {
			h, err := base64.StdEncoding.DecodeString(m.Text[v].HMAC)
			if err != nil {
				continue
			}
			mtag = append(mtag, h...)
		}

		mmtag := MessageTag(mtag)
		if mmtag != m.Tag {
			return "", nil, fmt.Errorf("Message tag failure")
		}

		if len(plaintext) < 64 {
			return "", nil, fmt.Errorf("Invalid plaintext size")
		}

		return "", plaintext[:len(plaintext)-64], nil
	}

	return "", nil, nil
}

func (me *Me) genFingerprint(nick string) string {
	me.lock()

	defer me.unlock()
	key := me.PublicKey[:]
	if nick != "" {
		if me.Buddies[nick] == nil {
			return ""
		}
		key = me.Buddies[nick].PublicKey[:]
	}

	fp := Sha512(key)
	fps := hex.EncodeToString(fp)
	fps = strings.ToUpper(fps)
	return fps[:40]
}

func fpspace(key string) string {
	str := []rune(key)

	if len(str) != 40 {
		return "bad key"
	}

	formatted := []rune{}
	for i, c := range str {
		if i != 0 && i%8 == 0 {
			formatted = append(formatted, rune(' '))
		}
		formatted = append(formatted, c)
	}

	return string(formatted)
}

func (me *Me) genSharedSecret(nick string) *MPStorage {
	var secret [32]byte

	curve25519.ScalarMult(&secret, &me.SecretKey, &me.Buddies[nick].PublicKey)
	shash := Sha512(secret[:])

	return &MPStorage{
		Message: shash[0:32],
		HMAC:    shash[32:64],
	}
}

func fixIV(s string) []byte {
	buf, _ := base64.StdEncoding.DecodeString(s)
	if len(buf) < 12 {
		return make([]byte, 16)
	}

	buf = append(buf[:12], []byte{0x00, 0x00, 0x00, 0x00}...)
	return buf
}

func newIV() string {
	buf := make([]byte, 12)
	rand.Read(buf)
	return base64.StdEncoding.EncodeToString(buf)
}

func DeriveKey(password string) []byte {
	bytes := sha256.Sum256([]byte(password))
	return bytes[:32]
}

func encryptAES(plaintext, key []byte, iv []byte) string {
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	ciphertext := make([]byte, len(plaintext))

	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext, plaintext)

	return base64.StdEncoding.EncodeToString(ciphertext)
}

func decryptAES(msg string, key []byte, iv []byte) []byte {
	ciphertext, err := base64.StdEncoding.DecodeString(msg)
	if err != nil {
		return []byte("malformed message")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	plaintext := make([]byte, len(ciphertext))

	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(plaintext, ciphertext)

	return plaintext
}

func NewMe(username string, profile string) (*Me, error) {
	me := &Me{}
	me.Name = username
	me.Buddies = make(map[string]*Buddy)
	me.keyMap = make(map[string]*time.Time)
	me.blacklist = make(map[string]bool)

	if profile != "" {
		b, err := base64.StdEncoding.DecodeString(profile)
		if err != nil {
			return nil, err
		}

		copy(me.SecretKey[:], b)
		curve25519.ScalarBaseMult(&me.PublicKey, &me.SecretKey)
	} else {
		me.GenerateKeys()
	}

	me._sendFunc = func([]byte) {}
	return me, nil
}

func (me *Me) ReceiveMessage(sender, message string) (string, []byte, error) {
	if me.MaximumMessageSize == 0 {
		me.MaximumMessageSize = 6000
	}

	var mt map[string]interface{}
	err := json.Unmarshal([]byte(message), &mt)
	if err != nil {
		return "", nil, err
	}

	if mt["type"] == "message" {
		var ans Answer
		json.Unmarshal([]byte(message), &ans)
		if cont := ans.Text[me.Name]; cont != nil {
			if len(cont.Message) > me.MaximumMessageSize {
				return "", nil, fmt.Errorf("message exceeded maximum size, refusing to decrypt")
			}
		}
	}

	me.lock()
	keyAuth, b, err := me.receiveMessage(sender, message, mt)
	me.unlock()
	return keyAuth, b, err
}

func (me *Me) SendMessage(message []byte) {
	me.lock()
	me.sendMessage(message)
	me.unlock()
}

func (me *Me) ClearBlacklist() {
	me.keyLock.Lock()
	me.blacklist = make(map[string]bool)
	me.keyLock.Unlock()
}

func (me *Me) NamesByFingerprint(fp string) []string {
	var allNames []string
	var matchedNames []string
	me.lock()
	for k := range me.Buddies {
		allNames = append(allNames, k)
	}
	me.unlock()

	for _, k := range allNames {
		if fp == me.genFingerprint(k) {
			matchedNames = append(matchedNames, k)
		}
	}

	sort.Strings(matchedNames)
	return matchedNames
}

func (me *Me) SortedNames() []string {
	var names []string
	for k := range me.Buddies {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

func (me *Me) DestroyUser(name string) {
	me.lock()

	delete(me.Buddies, name)
	delete(me.keyMap, name)
	me.unlock()
}

func (me *Me) saveProfile() string {
	return base64.StdEncoding.EncodeToString(me.SecretKey[:])
}

func (me *Me) SaveProfile() string {
	me.lock()
	b := me.saveProfile()
	me.unlock()
	return b
}

func (me *Me) Fingerprint(username string) string {
	fp := me.genFingerprint(username)
	return fp
}

func (me *Me) FingerprintUser(username string) (string, error) {
	me.lock()

	if me.Buddies[username] == nil {
		me.unlock()
		return "", fmt.Errorf("no user found")
	}

	me.unlock()
	fp := me.genFingerprint(username)
	return fpspace(fp), nil
}

func (me *Me) Shutdown() {
}

func (me *Me) IsSessionInitialized(nickname string) bool {
	me.lock()

	if me.Buddies[nickname] == nil {
		me.unlock()
		return false
	}

	if me.Buddies[nickname].MpSecretKey == nil {
		me.unlock()
		return false
	}

	me.unlock()
	return true
}
