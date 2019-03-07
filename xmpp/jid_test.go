package xmpp

import "testing"

var testData = [][2]string{
	{"testing testing", `testing\20testing`},
	{"some/kinda/nick", `some\2fkinda\2fnick`},
}

func TestEscape(t *testing.T) {
	for _, v := range testData {
		if esc := EscapeLocal(v[0]); esc != v[1] {
			t.Fatal("Got", esc, "should have been", v[1])
		}
	}
}

func TestUnescape(t *testing.T) {
	for _, v := range testData {
		if unesc := UnescapeLocal(v[1]); unesc != v[0] {
			t.Fatal("Got", unesc, "should have been", v[0])
		}
	}
}
