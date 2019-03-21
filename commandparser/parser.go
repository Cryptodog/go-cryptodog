package commandparser

import (
	"html/template"
	"io"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/superp00t/etc"
)

type HandlerFunc func(C)

type Handler struct {
	Fn          HandlerFunc
	Description string
}

type Parser struct {
	Prefix            rune
	MaxLen            int64
	MaxSpamPerMinute  int64
	cmds              map[string]*Handler
	cl                *sync.Mutex
	AntiSpam          *sync.Map
	lastCommandString string
	rgx               map[string]*Handler
}

type C struct {
	Room string
	From string
	Name string
	Src  string
	Args []string
}

func (c C) Int64(index int) int64 {
	if len(c.Args) <= index {
		return 0
	}

	i64, _ := strconv.ParseInt(c.Args[index], 0, 64)
	return i64
}

func (c C) String(index int) string {
	if len(c.Args) <= index {
		return ""
	}

	return c.Args[index]
}

func (c C) Float64(index int) float64 {
	// [... ... 3] index = 3
	if len(c.Args) <= index {
		return 0.0
	}

	f, _ := strconv.ParseFloat(c.Args[index], 64)
	return f
}

func (p *Parser) On(cmd, description string, cb HandlerFunc) {
	p.cl.Lock()
	p.cmds[cmd] = &Handler{
		cb,
		description,
	}
	p.cl.Unlock()
}

func (p *Parser) Rgx(cmd, description string, cb HandlerFunc) {
	p.cl.Lock()
	regexp.MustCompile(cmd)
	p.rgx[cmd] = &Handler{
		cb,
		description,
	}
	p.cl.Unlock()
}

type SpamData struct {
	LastCommand time.Time
	SpamScore   int64
}

func (p *Parser) Parse(room, from, data string) {
	invocation := C{}
	invocation.Src = data
	invocation.Room = room
	invocation.From = from

	if int64(len(data)) > p.MaxLen {
		return
	}

	usID := from + "@" + room

	spm, ok := p.AntiSpam.Load(usID)
	if !ok {
		spa := &SpamData{
			LastCommand: time.Now(),
			SpamScore:   10,
		}

		p.AntiSpam.Store(usID, spa)

		go func() {
			for float64(spa.SpamScore) > 1 {
				time.Sleep(40 * time.Second)
				spa.SpamScore = int64(
					float64(spa.SpamScore) * .75,
				)
			}
			spa.SpamScore = 0
			p.AntiSpam.Delete(usID)
		}()
	} else {
		spa := spm.(*SpamData)

		if time.Since(spa.LastCommand) < time.Minute && spa.SpamScore > p.MaxSpamPerMinute {
			return
		}

		spa.LastCommand = time.Now()

		multiplier := 1
		if p.lastCommandString == data {
			multiplier = 2
		}

		spa.SpamScore += 10 * int64(multiplier)
	}

	prs := etc.FromString(data)
	rn, _, err := prs.ReadRune()
	if err != nil {
		return
	}

	if rn == p.Prefix {
		name := etc.NewBuffer()

		eof := false

		for {
			rn, _, err := prs.ReadRune()
			if rn == ' ' {
				break
			}
			if err == io.EOF {
				eof = true
				break
			}

			name.WriteRune(rn)
		}

		invocation.Name = name.ToString()

		if !eof {
			quoteState := false
			argBuf := etc.NewBuffer()

			for {
				rn, _, err := prs.ReadRune()
				if err == io.EOF {
					break
				}

				// A typical argument is terminated by a space
				if rn == ' ' && !quoteState {
					invocation.Args = append(
						invocation.Args,
						argBuf.ToString(),
					)
					argBuf = etc.NewBuffer()
					continue
				}

				// Delineates the end of a quoted asrgument.
				if rn == '"' && quoteState {
					quoteState = false
					invocation.Args = append(
						invocation.Args,
						argBuf.ToString(),
					)
					argBuf = etc.NewBuffer()
					continue
				}

				// The beginning of a quoted argument.
				if rn == '"' && !quoteState {
					quoteState = true
					continue
				}

				argBuf.WriteRune(rn)
			}

			if argBuf.Size() > 0 {
				invocation.Args = append(
					invocation.Args,
					argBuf.ToString(),
				)
			}
		}

		// finally, we can process the invocation.
		p.cl.Lock()
		fn := p.cmds[invocation.Name]
		p.cl.Unlock()

		if fn != nil {
			fn.Fn(invocation)
		}
	} else {
		p.cl.Lock()
		for k, v := range p.rgx {
			rgs := regexp.MustCompile(k).FindAllString(data, -1)
			if len(rgs) > 0 {
				inv := invocation
				inv.Args = rgs
				go v.Fn(inv)
				break
			}
		}
		p.cl.Unlock()
	}
}

func New() *Parser {
	p := new(Parser)
	p.cl = new(sync.Mutex)
	p.cmds = make(map[string]*Handler)
	p.Prefix = '.'
	p.MaxLen = 2048
	p.MaxSpamPerMinute = 60
	p.AntiSpam = new(sync.Map)
	return p
}

func (p *Parser) Help() HelpCommands {
	hc := HelpCommands{}
	for k, v := range p.cmds {
		hc.Commands = append(hc.Commands, HelpCommand{
			Command:     string([]rune{p.Prefix}) + k,
			Description: v.Description,
		})
	}

	sort.Sort(hc)

	return hc
}

func (hc HelpCommands) Len() int {
	return len(hc.Commands)
}

func (hc HelpCommands) Less(i, j int) bool {
	return hc.Commands[i].Command < hc.Commands[j].Command
}

func (hc HelpCommands) Swap(i, j int) {
	it := hc.Commands[i]
	jt := hc.Commands[j]

	hc.Commands[i] = jt
	hc.Commands[j] = it
}

type HelpCommands struct {
	Header   template.HTML
	Commands []HelpCommand
}

type HelpCommand struct {
	Command     string
	Description string
}
