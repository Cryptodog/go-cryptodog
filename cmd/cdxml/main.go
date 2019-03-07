package main

import (
	"github.com/Cryptodog/go-cryptodog/dog"
	"github.com/superp00t/etc/yo"
)

func main() {
	yo.Stringf("r", "room", "room", "lobby")
	yo.Stringf("n", "nick", "nickname", "tracer")

	yo.AddSubroutine("tap", []string{}, "prints xml", func(ar []string) {
		cn := dog.New()
		cn.Opts = dog.DMDisabled | dog.DebugXMPP

		cn.On(dog.Connected, func(e dog.Event) {
			cn.JoinRoom(yo.StringG("r"), yo.StringG("n"))
		})

		cn.Run()
	})

	yo.Init()
}
