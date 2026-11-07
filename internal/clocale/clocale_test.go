package clocale

import (
	"strings"
	"testing"
	"time"
)

func TestTheFormatsFollowEachLocale(t *testing.T) {
	at := time.Date(2026, time.March, 4, 13, 5, 6, 0, time.UTC)
	c, err := Format(Formats{}, at)
	if err != nil {
		t.Fatal(err)
	}
	if c.Number != "1234567.89" || !strings.Contains(c.Date, "13:05:06") || !strings.Contains(c.Date, "2026") {
		t.Fatalf("the C locale: %+v", c)
	}
	us, err := Format(Formats{Time: "en_US.UTF-8", Numeric: "en_US.UTF-8", Monetary: "en_US.UTF-8"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if us.Number != "1,234,567.89" || us.Price != "$1,234.50" || !strings.Contains(us.Date, "PM") {
		t.Fatalf("en_US: %+v", us)
	}
	gb, err := Format(Formats{Time: "en_GB.UTF-8", Numeric: "C", Monetary: "en_GB.UTF-8"}, at)
	if err != nil {
		t.Fatal(err)
	}
	if gb.Number != "1234567.89" || gb.Price != "£1,234.50" || strings.Contains(gb.Date, "PM") {
		t.Fatalf("en_GB time and money, C numbers: %+v", gb)
	}
	if _, err := Format(Formats{Time: "xx_NOPE.UTF-8"}, at); err == nil || !strings.Contains(err.Error(), "xx_NOPE") {
		t.Fatalf("an unknown locale: %v", err)
	}
}
