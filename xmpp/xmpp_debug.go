package xmpp

import (
	"fmt"
	"strings"

	xj "github.com/basgys/goxml2json"
)

func PrintTree(s string) {
	xml := strings.NewReader(s)

	json, err := xj.Convert(xml)
	if err != nil {
		panic(err)
	}

	fmt.Println(json)
}
