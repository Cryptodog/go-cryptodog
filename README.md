![godog](https://img.ikrypto.club/2ENO.png)

# go-cryptodog

![license](https://img.shields.io/badge/License-MIT-blue.svg)

go-cryptodog is a general-purpose Golang API for writing programs that interact with [Cryptodog](https://crypto.dog).

This software has not been audited, and probably never will be. Use at your own risk.

### Example

```go
package main

import (
  "fmt"
  "github.com/Cryptodog/go-cryptodog/dog"
)

func main() {
  d := dog.New()

  d.On(dog.Connected, func(e dog.Event) {
    fmt.Println("Connected!")
    d.JoinRoom("testingroom", "DemoBot")
  })

  d.On(dog.Disconnected, func(e dog.Event) {
    fmt.Println("Connected!")
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
  fmt.Println(d.Run())
}
```