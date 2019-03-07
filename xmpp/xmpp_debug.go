package xmpp

import (
	"encoding/json"
	"strings"

	"github.com/beevik/etree"
	"github.com/superp00t/etc/yo"
)

func PrintTree(s string) {
	doc := etree.NewDocument()

	err := doc.ReadFromString(s)
	if err != nil {
		return
	}

	var bodyReplace string
	replaceStr := ">>>"

	msg := doc.SelectElement("message")
	if msg != nil {
		bod := msg.SelectElement("body")
		if bod != nil {
			body := bod.Text()

			var i map[string]interface{}

			json.Unmarshal([]byte(body), &i)
			ind, _ := json.MarshalIndent(i, "", "  ")
			bodyReplace = string(ind)
			bod.SetText(replaceStr)
		}
	}

	doc.Indent(2)

	str, _ := doc.WriteToString()

	str = strings.Replace(str, replaceStr, bodyReplace, -1)

	yo.Ok(str)
}
