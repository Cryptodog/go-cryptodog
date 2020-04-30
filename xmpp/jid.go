package xmpp

import (
	"fmt"
	"io"
	"strings"

	"github.com/superp00t/etc"
)

type JID struct {
	Local string
	Host  string
	Node  string
}

func ParseJID(s string) (JID, error) {
	if !strings.Contains(s, "@") {
		return JID{}, fmt.Errorf("xmpp: could not parse jid without @")
	}

	j := JID{}

	spl := strings.SplitN(s, "@", 2)
	j.Local = spl[0]

	if strings.Contains(spl[1], "/") {
		split := strings.SplitN(spl[1], "/", 2)
		j.Host = split[0]
		j.Node = split[1]
	} else {
		j.Host = spl[1]
	}

	if j.Node != "" {
		j.Node = UnescapeLocal(j.Node)
	}

	return j, nil
}

func (j JID) String() string {
	str := j.Local + "@" + j.Host

	if j.Node != "" {
		str += "/" + j.Node
	}

	return str
}

func UnescapeLocal(s string) string {
	e := etc.FromString(s)
	o := etc.NewBuffer()

mainLoop:
	for e.Available() > 0 {
		rn, _, err := e.ReadRune()
		if err == io.EOF {
			return o.ToString()
		}

		if rn == '\\' {
			chr := `\` + e.ReadFixedString(2)

			for k, v := range charsLookup {
				if v == chr {
					o.WriteRune(k)
					goto mainLoop
				}
			}
		}

		o.WriteRune(rn)
	}

	return o.ToString()
}

var charsLookup = map[rune]string{
	' ':  "\\20",
	'"':  "\\22",
	'&':  "\\26",
	'\'': "\\27",
	'/':  "\\2f",
	':':  "\\3a",
	'<':  "\\3c",
	'>':  "\\3e",
	'@':  "\\40",
	'\\': "\\5c",
}

func EscapeLocal(s string) string {
	out := etc.NewBuffer()
	for _, v := range []rune(s) {
		if charsLookup[v] != "" {
			out.Write([]byte(charsLookup[v]))
		} else {
			out.WriteRune(v)
		}
	}
	return out.ToString()
}
