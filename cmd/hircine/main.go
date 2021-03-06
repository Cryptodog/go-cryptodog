package main

import (
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/Cryptodog/go-cryptodog/dog"
	"github.com/fatih/color"

	"github.com/davecgh/go-spew/spew"

	"github.com/go-irc/irc"
	"github.com/superp00t/etc"
	"github.com/superp00t/etc/yo"
)

func main() {
	yo.Stringf(
		"s",
		"storage",
		"where files are stored",
		etc.LocalDirectory().Concat("hircine").Render())

	yo.Stringf(
		"l",
		"listen",
		"where to listen",
		":6667",
	)

	yo.Main("server", _main)
	yo.Init()
}

func _main(args []string) {
	spew.Config.DisableMethods = true

	color.Set(color.FgGreen)
	os.Stdout.Write(AsciiArt)
	color.Unset()
	l, err := net.Listen("tcp", yo.StringG("l"))
	if err != nil {
		yo.Fatal(err)
	}

	yo.Ok("hIRCine listening at", yo.StringG("l"))

	for {
		cn, err := l.Accept()
		if err != nil {
			yo.Fatal(err)
		}

		go handleConn(cn)
	}
}

type conn struct {
	cd *dog.Conn

	c     net.Conn
	r     *irc.Reader
	w     *irc.Writer
	nick  string
	room  string
	flags int
}

func handleConn(cn net.Conn) {
	ir := irc.NewReader(cn)
	iw := irc.NewWriter(cn)

	con := &conn{
		c: cn,
		r: ir,
		w: iw,
	}

	for {
		msg, err := ir.ReadMessage()
		if err != nil {
			yo.Warn(err)
			return
		}

		quit := con.handleCommand(msg)
		if quit {
			con.c.Close()
			return
		}
	}
}

func (c *conn) handleCommand(msg *irc.Message) bool {
	HOST := "crypto.dog"

	switch msg.Command {
	case "NICK":
		c.nick = msg.Params[0]
		c.flags++
	case "USER":
		c.notice("AUTH", "Doing login stuffs")
		c.flags++
	case "JOIN":
		c.joinRoom(msg.Params)
	case "PRIVMSG":
		if len(msg.Params) > 1 {
			targetMessage := msg.Params[1]
			if msg.Params[0][0] == '#' {
				targetRoom := msg.Params[0][1:]
				yo.Ok("Sending message", targetRoom, targetMessage)
				c.cd.GM(targetRoom, targetMessage)
			} else {
				targetUser := msg.Params[0]
				c.cd.DM(c.room, targetUser, targetMessage)
			}
		}
	case "QUIT":
		c.cd.Disconnect()
		c.cd = nil
		return true
	case "WHO":

	case "PING":
		fmt.Fprintf(c.c, ":%s PONG %s :%s\n", HOST, HOST, HOST)
	}

	if c.flags == 2 {
		c.flags = 0
		c.sendAuth()
	}

	return false
}

func displayName(st string) string {
	return strings.Replace(st, " ", " ", -1)
}

func (c *conn) notice(t, value string) {
	c.w.WriteMessage(&irc.Message{
		Command: "NOTICE",
		Params: []string{
			t,
			":***",
			value,
		},
	})
}

func (c *conn) sendAuth() {
	fmt.Fprintf(c.c, ":%s 001 %s :Welcome to the Hircine IRC gateway %s\n", "crypto.dog", c.nick, c.nick)
	fmt.Fprintf(c.c, ":%s MODE %s :+i\n", c.nick, c.nick)
}

func (c *conn) joinRoom(s []string) {
	if len(s) == 0 {
		return
	}

	room := strings.TrimLeft(s[0], "#")

	c.room = room

	c.w.WriteMessage(&irc.Message{
		Prefix: &irc.Prefix{
			Name: displayName(c.nick),
		},
		Command: "JOIN",
		Params:  []string{"#" + room},
	})

	if c.cd == nil {
		c.cd = dog.New()
		c.cd.Opts = dog.Human
		c.cd.DB = dog.Disk(yo.StringG("s"))
		c.cd.SetMods([]string{
			"94D6D86FB4F2B2EE7AC2A639ABFBBC390113DD0D",
		})
		c.cd.DB.Delete("rooms")

		c.cd.On(dog.NicknameInUse, func(e dog.Event) {
			yo.Warn("nickname in use")
		})

		c.cd.On(dog.Connected, func(e dog.Event) {
			c.cd.JoinRoom(room, c.nick)
		})

		c.cd.On(dog.RoomJoined, func(e dog.Event) {
			rm := c.cd.GetRoom(e.Room)
			users := rm.GetUsernames()
			HOST := "crypto.dog"
			for _, v := range users {
				fmt.Fprintf(c.c, ":%s 352 %s #%s %s %s %s %s %s\n", HOST, c.nick, room, v, HOST, HOST, "*", "0"+v)
			}

			fmt.Fprintf(c.c, ":%s 315 %s #%s :End of /WHO list\n", HOST, c.nick, room)

			c.notice("AUTH", "you joined "+e.Room)
			for _, v := range c.cd.GetRoom(e.Room).GetUsernames() {
				c.w.WriteMessage(&irc.Message{
					Prefix: &irc.Prefix{
						Name: displayName(v),
					},
					Command: "JOIN",
					Params:  []string{"#" + e.Room},
				})
			}
		})

		c.cd.On(dog.UserJoined, func(d dog.Event) {
			c.w.WriteMessage(&irc.Message{
				Prefix: &irc.Prefix{
					Name: displayName(d.User),
				},
				Command: "JOIN",
				Params:  []string{"#" + d.Room},
			})
		})

		c.cd.On(dog.UserLeft, func(d dog.Event) {
			c.w.WriteMessage(&irc.Message{
				Prefix: &irc.Prefix{
					Name: displayName(d.User),
				},
				Command: "QUIT",
				Params:  []string{"#" + d.Room},
			})
		})

		c.cd.On(dog.GroupMessage, func(e dog.Event) {
			c.sendPrivMsgSplit(e.User, "#"+e.Room, e.Body)
		})

		c.cd.On(dog.PrivateMessage, func(e dog.Event) {
			c.sendPrivMsgSplit(e.User, c.nick, e.Body)
		})

		go func() {
			err := c.cd.Run()
			if err != nil {
				yo.Fatal(err)
			}
		}()
	} else {
		c.cd.JoinRoom(room, c.nick)
	}
}

func (c *conn) sendPrivMsgSplit(user, target, body string) {
	strs := strings.Split(
		strings.Replace(body, "\r", "", -1), "\n")

	for _, m := range strs {
		c.w.WriteMessage(&irc.Message{
			Prefix: &irc.Prefix{
				Name: displayName(user),
			},
			Command: "PRIVMSG",
			Params: []string{
				target,
				m,
			},
		})
	}
}
