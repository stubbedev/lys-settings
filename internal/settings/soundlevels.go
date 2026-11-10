package settings

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/stubbedev/lys-desktop/service/pulse"
	"github.com/stubbedev/lys-lib/intconv"
	"github.com/stubbedev/lys-lib/pulse/native"
	"github.com/stubbedev/lys-lib/render"
	"github.com/stubbedev/lys-ui/widget"
)

const (
	meterFloorDB   = -60.0
	toneRate       = 48000
	toneFrames     = toneRate / 2
	toneFadeFrames = toneRate / 100
	toneHz         = 440.0
	toneLFEHz      = 60.0
	toneLevel      = 0.25
)

var meterSpec = pulse.CaptureSpec{Name: "lys-settings input level", Rate: 8000, Channels: 1, FragmentFrames: 400}

func peakLevel(levels []float64) float64 {
	peak := 0.0
	for _, l := range levels {
		peak = max(peak, l)
	}
	return peak
}

func scaledTo(levels []float64, level float64) []float64 {
	out := make([]float64, len(levels))
	peak := peakLevel(levels)
	for i, l := range levels {
		if peak == 0 {
			out[i] = level
			continue
		}
		out[i] = l * level / peak
	}
	return out
}

func channelSide(p native.ChannelPosition) int {
	switch p {
	case native.ChannelFrontLeft, native.ChannelRearLeft, native.ChannelFrontLeftOfCenter,
		native.ChannelSideLeft, native.ChannelTopFrontLeft, native.ChannelTopRearLeft:
		return -1
	case native.ChannelFrontRight, native.ChannelRearRight, native.ChannelFrontRightOfCenter,
		native.ChannelSideRight, native.ChannelTopFrontRight, native.ChannelTopRearRight:
		return 1
	}
	return 0
}

func sideLevels(levels []float64, m native.ChannelMap) (left, right float64, stereo bool) {
	var nl, nr int
	for i, p := range m[:min(len(m), len(levels))] {
		switch channelSide(p) {
		case -1:
			left += levels[i]
			nl++
		case 1:
			right += levels[i]
			nr++
		}
	}
	if nl == 0 || nr == 0 {
		return 0, 0, false
	}
	return left / float64(nl), right / float64(nr), true
}

func balanceOf(levels []float64, m native.ChannelMap) (float64, bool) {
	left, right, stereo := sideLevels(levels, m)
	switch {
	case !stereo:
		return 0, false
	case left == right:
		return 0, true
	case left > right:
		return right/left - 1, true
	}
	return 1 - left/right, true
}

func withBalance(levels []float64, m native.ChannelMap, balance float64) []float64 {
	out := append([]float64(nil), levels...)
	left, right, stereo := sideLevels(levels, m)
	if !stereo {
		return out
	}
	peak := max(left, right)
	newLeft, newRight := peak, peak
	if balance <= 0 {
		newRight = (balance + 1) * peak
	} else {
		newLeft = (1 - balance) * peak
	}
	for i, p := range m[:min(len(m), len(levels))] {
		switch channelSide(p) {
		case -1:
			out[i] = resided(levels[i], left, newLeft)
		case 1:
			out[i] = resided(levels[i], right, newRight)
		}
	}
	return out
}

func resided(level, side, next float64) float64 {
	if side == 0 {
		return next
	}
	return level * next / side
}

func testToneName(m native.ChannelMap, channel int) string {
	positions := make([]string, len(m))
	for i, p := range m {
		positions[i] = strconv.Itoa(int(p))
	}
	return "lys-settings:test-tone:" + strings.Join(positions, ",") + ":" + strconv.Itoa(channel)
}

func testTone(m native.ChannelMap, channel int) (native.Sample, error) {
	channels, err := intconv.Exact[uint8](len(m))
	if err != nil || channel < 0 || channel >= len(m) {
		return native.Sample{}, fmt.Errorf("a test tone on channel %d of %d", channel, len(m))
	}
	hz := toneHz
	if m[channel] == native.ChannelLFE {
		hz = toneLFEHz
	}
	samples := make([]int16, toneFrames*len(m))
	for f := range toneFrames {
		envelope := min(1, float64(f)/toneFadeFrames, float64(toneFrames-1-f)/toneFadeFrames)
		samples[f*len(m)+channel] = int16(math.Round(toneLevel * envelope * math.MaxInt16 * math.Sin(2*math.Pi*hz*float64(f)/toneRate)))
	}
	data := make([]byte, 2*len(samples))
	if _, err := binary.Encode(data, binary.LittleEndian, samples); err != nil {
		return native.Sample{}, fmt.Errorf("encode a test tone: %w", err)
	}
	return native.Sample{
		SampleSpec: native.SampleSpec{Format: native.SampleS16LE, Channels: channels, Rate: toneRate},
		ChannelMap: append(native.ChannelMap(nil), m...),
		Data:       data,
	}, nil
}

func playTestTone(ctx context.Context, svc *pulse.Service, sink string, m native.ChannelMap, channel int) error {
	play := native.SamplePlay{
		Name:   testToneName(m, channel),
		Sink:   sink,
		Volume: native.VolumeNorm,
		Props:  native.PropList{"media.role": "test", "media.name": "Test tone", "application.name": "lys-settings"},
	}
	return svc.PlaySample(ctx, play, func() (native.Sample, error) { return testTone(m, channel) })
}

func meterLevel(peak int) float64 {
	if peak <= 0 {
		return 0
	}
	db := 20 * math.Log10(float64(peak)/-math.MinInt16)
	return min(1, max(0, 1-db/meterFloorDB))
}

type levelMeter struct {
	*widget.LevelBar
	invoke  func(func())
	level   atomic.Uint64
	queued  atomic.Bool
	redraws int
}

func newLevelMeter(k *kit) *levelMeter {
	return &levelMeter{LevelBar: widget.NewLevelBar(0), invoke: k.invoke}
}

func (m *levelMeter) feed(pcm []byte) {
	peak := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(pcm[i]) | int(pcm[i+1])<<8
		if v > math.MaxInt16 {
			v = 1<<16 - v
		}
		peak = max(peak, v)
	}
	level := math.Float64bits(meterLevel(peak))
	if m.level.Swap(level) != level && m.queued.CompareAndSwap(false, true) {
		m.invoke(m.update)
	}
}

func (m *levelMeter) update() {
	before := m.Value()
	m.SetValue(math.Float64frombits(m.level.Load()))
	if m.Value() == before {
		m.queued.Store(false)
		return
	}
	m.redraws++
}

func (m *levelMeter) Paint(cv *render.Canvas) {
	m.LevelBar.Paint(cv)
	m.queued.Store(false)
	if math.Float64frombits(m.level.Load()) != m.Value() && m.queued.CompareAndSwap(false, true) {
		m.invoke(m.update)
	}
}
