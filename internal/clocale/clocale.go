package clocale

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ebitengine/purego"
	"github.com/stubbedev/lys-lib/clib"
	"github.com/stubbedev/lys-lib/intconv"
)

const (
	lcNumeric  = 1
	lcTime     = 2
	lcMonetary = 4

	radixChar    = lcNumeric << 16
	thousandsSep = lcNumeric<<16 | 1
	currency     = lcMonetary<<16 | 15
	monDecimal   = lcMonetary<<16 | 2
	monThousands = lcMonetary<<16 | 3

	timeBuf = 256
)

type tm struct {
	sec, min, hour, mday, mon, year, wday, yday, isdst int32
	gmtoff                                             int64
	zone                                               *byte
}

var libc = sync.OnceValues(func() (*funcs, error) {
	h, err := clib.Open(clib.SystemCandidates("libc.so.6"))
	if err != nil {
		return nil, fmt.Errorf("clocale: open the C library: %w", err)
	}
	f := &funcs{}
	purego.RegisterLibFunc(&f.newlocale, h, "newlocale")
	purego.RegisterLibFunc(&f.freelocale, h, "freelocale")
	purego.RegisterLibFunc(&f.strftimeL, h, "strftime_l")
	purego.RegisterLibFunc(&f.langinfoL, h, "nl_langinfo_l")
	return f, nil
})

type funcs struct {
	newlocale  func(mask int32, name string, base uintptr) uintptr
	freelocale func(loc uintptr)
	strftimeL  func(buf *byte, max uintptr, format string, t *tm, loc uintptr) uintptr
	langinfoL  func(item int32, loc uintptr) *byte
}

// Formats is a locale per category, "" meaning the C locale.
type Formats struct {
	Time, Numeric, Monetary string
}

// Preview is a date and time, a number and a price as the formats write
// them.
type Preview struct {
	Date, Number, Price string
}

// Format writes at, 1234567.89 and 1234.5 in f's locales; a locale the
// system does not have is an error naming it.
func Format(f Formats, at time.Time) (Preview, error) {
	c, err := libc()
	if err != nil {
		return Preview{}, err
	}
	open := func(category int32, name string) (uintptr, error) {
		if name == "" {
			name = "C"
		}
		loc := c.newlocale(1<<category, name, 0)
		if loc == 0 {
			return 0, fmt.Errorf("clocale: the system has no locale %q", name)
		}
		return loc, nil
	}
	var p Preview
	tl, err := open(lcTime, f.Time)
	if err != nil {
		return p, err
	}
	defer c.freelocale(tl)
	nl, err := open(lcNumeric, f.Numeric)
	if err != nil {
		return p, err
	}
	defer c.freelocale(nl)
	ml, err := open(lcMonetary, f.Monetary)
	if err != nil {
		return p, err
	}
	defer c.freelocale(ml)
	c32 := intconv.Must[int32, int]
	t := tm{sec: c32(at.Second()), min: c32(at.Minute()), hour: c32(at.Hour()), mday: c32(at.Day()), mon: c32(int(at.Month()) - 1), year: c32(at.Year() - 1900), wday: c32(int(at.Weekday())), yday: c32(at.YearDay() - 1)}
	if at.IsDST() {
		t.isdst = 1
	}
	zone, offset := at.Zone()
	zoneName := append([]byte(zone), 0)
	t.zone, t.gmtoff = &zoneName[0], int64(offset)
	buf := make([]byte, timeBuf)
	n := c.strftimeL(&buf[0], uintptr(len(buf)), "%c", &t, tl)
	p.Date = string(buf[:n])
	runtime.KeepAlive(zoneName)
	p.Number = group("1234567", "89", clib.GoString(c.langinfoL(thousandsSep, nl)), clib.GoString(c.langinfoL(radixChar, nl)))
	amount := group("1234", "50", clib.GoString(c.langinfoL(monThousands, ml)), clib.GoString(c.langinfoL(monDecimal, ml)))
	symbol := clib.GoString(c.langinfoL(currency, ml))
	switch {
	case strings.HasPrefix(symbol, "-"):
		p.Price = symbol[1:] + amount
	case strings.HasPrefix(symbol, "+"):
		p.Price = amount + " " + symbol[1:]
	case strings.HasPrefix(symbol, "."):
		p.Price = strings.Replace(amount, clib.GoString(c.langinfoL(monDecimal, ml)), symbol[1:], 1)
	default:
		p.Price = amount
	}
	return p, nil
}

func group(whole, fraction, sep, radix string) string {
	var b strings.Builder
	for i, r := range whole {
		if i > 0 && (len(whole)-i)%3 == 0 {
			b.WriteString(sep)
		}
		b.WriteRune(r)
	}
	if radix == "" {
		radix = "."
	}
	return b.String() + radix + fraction
}
