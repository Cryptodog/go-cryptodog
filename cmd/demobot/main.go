package main

import (
	"fmt"
	"reflect"
	"time"

	"github.com/Cryptodog/go-cryptodog/dog"
)

func ReverseSlice(s interface{}) {
	size := reflect.ValueOf(s).Len()
	swap := reflect.Swapper(s)
	for i, j := 0, size-1; i < j; i, j = i+1, j-1 {
		swap(i, j)
	}
}

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

	// d.Proxy = "127.0.0.1:9150"

	d.On(dog.Connected, func(e dog.Event) {
		fmt.Println("Connected!")
		d.JoinRoom("lobby", "DemoBot")
	})

	d.On(dog.RoomJoined, func(e dog.Event) {
		fmt.Println("Joined room", e.Room)
		inject := dog.EncodeBEX([]dog.BEX{
			{Header: dog.FLAG_ME_AS_BOT, Color: "ff69b4"},
		})

		fmt.Println("Injecting bex...")

		ReverseSlice(inject)

		d.Group(e.Room, inject)

		time.Sleep(500 * time.Millisecond)
		d.GM(e.Room, "reverse that")
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
