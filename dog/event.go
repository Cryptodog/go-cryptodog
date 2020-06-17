package dog

type EventHandler func(Event)

type EventType int

const (
	Any EventType = iota
	RateLimit
	NicknameInUse
	Disconnected
	Connected
	UserJoined
	UserLeft
	GroupMessage
	PrivateMessage
	SMPQuestion
	SMPAnswer
	SMPSuccess
	SMPFailure
	Composing
	Paused
	ColorModify
	FileAttachment
	SubscribedToModerator
	RoomJoined
	WebRTCCapable
	WebRTCOffer
	WebRTCAnswer
	WebRTCIceCandidate
	InvalidGroupMessage
	Roster
)

// Event describes
type Event struct {
	Type    EventType
	Private bool
	Room    string
	User    string
	Body    string
	File    *File
}

// On registers a function that will handle an Event.
func (c *Conn) On(_type EventType, handler EventHandler) {
	c.handlers[_type] = append(c.handlers[_type], handler)
}

func (c *Conn) emit(evt Event) {
	c.handlersLock.Lock()
	defer c.handlersLock.Unlock()
	for _, v := range c.handlers[Any] {
		go v(evt)
	}

	for _, v := range c.handlers[evt.Type] {
		go v(evt)
	}
}
