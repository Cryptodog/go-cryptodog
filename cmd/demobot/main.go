package main

import (
	"fmt"

	"github.com/Cryptodog/go-cryptodog/dog"
)

func main() {
	d := dog.New()

	// Sets long-term storage system to a local folder
	//
	// %USERROFILE%\AppData\Local\DemoBot\ on Windows
	// $HOME/.local/share/DemoBot/ on Unix-like systems
	//
	// Completely optional. if d.DB is not set, it will default to an in-memory store.
	d.DB = dog.FolderStore("DemoBot")

	// Alternatively:
	// d.DB = dog.Disk("/full/path/")

	// Want to connect via Tor/Tor Browser's process? (Note: won't work when compiled to webassembly.)
	// d.Proxy = "127.0.0.1:9150"

	d.On(dog.Connected, func(e dog.Event) {
		fmt.Println("Connected!")
		d.JoinRoom("elysium", "DemoBot")
	})

	d.On(dog.RoomJoined, func(e dog.Event) {
		fmt.Println("Joined room", e.Room)
	})

	d.On(dog.NicknameInUse, func(e dog.Event) {
		fmt.Println("Nickname is in use.")
		d.Disconnect()
	})

	// If this happens, the bot will automatically try to reconnect.
	d.On(dog.Disconnected, func(e dog.Event) {
		fmt.Println("Disconnected :(")
	})

	d.On(dog.GroupMessage, func(event dog.Event) {
		if event.Body == "hello" {
			d.GMf(event.Room, "Hello, %s! How are you today?", event.User)
		}

		if event.Body == "please die" {
			d.GM(event.Room, "sure thing!")

			// Causes d.Run() to return nil (graceful exit)
			d.Disconnect()
		}
	})

	// Blocks until error is returned.
	if err := d.Run(); err != nil {
		fmt.Println(err)
	}
}
